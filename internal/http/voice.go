package http

import (
	"bytes"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/TryEarful/earful/internal/ai"
	"github.com/TryEarful/earful/internal/antibot"
	"github.com/TryEarful/earful/internal/store"
	"github.com/TryEarful/earful/internal/voice"
	"github.com/TryEarful/earful/internal/ws"
)

// Voice answering, ADR-0004's server path (M5-T2).
//
// A respondent's browser opens a socket, sends 16 kHz mono PCM while the
// mic is on, and sends {"action":"stop"} when they finish. The server
// holds the audio in memory, transcribes it once, streams the transcript
// back as it arrives, and drops the audio. Nothing here writes audio
// anywhere: the only package that touches the bytes is internal/voice,
// which has a build-time test proving it cannot.
//
// The whole feature is an enhancement. The textarea is rendered and
// submittable with or without it, so every failure path below ends in
// "type your answer instead" rather than an error page (story 38).

// voiceSocketLifetime bounds one answering session. Far under Cloud Run's
// 60-minute cut, because a spoken answer is a matter of seconds and an
// idle socket is just cost.
const voiceSocketLifetime = 10 * time.Minute

// voiceChunkBytes bounds one binary frame: the worklet sends ~8 KB every
// quarter second, so this is generous without being unbounded.
const voiceChunkBytes = 256 << 10

// voiceSocket serves the anonymous share-link path.
func (s *server) voiceSocket(w http.ResponseWriter, r *http.Request) {
	surveyID, err := uuid.Parse(r.PathValue("surveyID"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	survey, err := s.surveys.PublicSurvey(r.Context(), surveyID)
	if err != nil || !survey.IsAnonymous {
		http.NotFound(w, r)
		return
	}
	s.serveVoice(w, r, survey)
}

// participantVoiceSocket serves the personal-link path. The token is the
// credential, exactly as it is for the form itself.
func (s *server) participantVoiceSocket(w http.ResponseWriter, r *http.Request) {
	resolved, err := s.surveys.ParticipantByToken(r.Context(), r.PathValue("token"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	survey, err := s.surveys.PublicSurvey(r.Context(), resolved.SurveyID)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	s.serveVoice(w, r, survey)
}

func (s *server) serveVoice(w http.ResponseWriter, r *http.Request, survey store.PublicSurvey) {
	if !survey.State().AcceptsResponses(s.clock.Now()) {
		http.NotFound(w, r)
		return
	}
	if !ai.Supports(s.ai, ai.OpTranscribe) {
		// No transcription configured: the page should not have offered
		// the mic, so this is either a stale page or a probe.
		http.Error(w, "voice is not available", http.StatusNotImplemented)
		return
	}
	// Rate-limit before the upgrade: a refused handshake costs nothing,
	// an accepted one costs a goroutine and a buffer.
	if !s.limitVoice.Allow(s.clientIP(r) + "|" + survey.ID.String()) {
		http.Error(w, "too many voice sessions", http.StatusTooManyRequests)
		return
	}

	conn, err := ws.Accept(w, r, ws.Options{
		MaxMessageBytes: voiceChunkBytes,
		MaxLifetime:     voiceSocketLifetime,
	})
	if err != nil {
		// Accept has already answered the request (a bad Origin, say).
		s.logger.Debug("voice socket not accepted", "error", err)
		return
	}
	defer conn.Close()

	s.runVoiceSession(conn, survey)
}

// runVoiceSession is the protocol loop: start → audio → stop → transcript.
func (s *server) runVoiceSession(conn *ws.Conn, survey store.PublicSurvey) {
	buf := voice.NewBuffer(s.voiceAnswerSeconds())
	// Whatever happens — a clean stop, a dropped connection, a panic
	// unwinding the handler — the audio does not outlive this function.
	defer buf.Reset()

	var (
		session  string // the form nonce: one respondent filling one form
		language string
		started  bool
	)

	for {
		msg, err := conn.Receive()
		if errors.Is(err, ws.ErrClosed) {
			return
		}
		if err != nil {
			s.logger.Debug("voice socket read failed", "error", err)
			return
		}

		if msg.Binary {
			if !started {
				continue // audio before start: ignore rather than trust
			}
			if err := buf.Append(msg.Data); err != nil {
				// The cap is reached: transcribe what we have rather than
				// throwing away what the respondent already said.
				s.finishVoiceTake(conn, survey, buf, session, language)
				return
			}
			continue
		}

		switch msg.Control.Action {
		case "start":
			if !s.voiceSessionAllowed(conn, survey, msg.Control) {
				return
			}
			session = msg.Control.Param("nonce")
			language = msg.Control.Param("lang")
			started = true
			buf.Reset()
		case "stop":
			if !started {
				return
			}
			s.finishVoiceTake(conn, survey, buf, session, language)
			return
		default:
			return
		}
	}
}

// voiceSessionAllowed applies the same gate the form itself applies: the
// signed render timestamp proves this socket belongs to a page we served.
// A per-answer minimum fill time makes no sense here (speaking starts
// immediately), so the token is checked for authenticity and age only.
func (s *server) voiceSessionAllowed(conn *ws.Conn, survey store.PublicSurvey, control ws.Control) bool {
	if err := s.formTokens.Check(survey.ID.String(), control.Param("token"), 0); err != nil {
		if errors.Is(err, antibot.ErrFormTokenInvalid) {
			_ = conn.Fail("stale", "This page has been open a while. Reload it to use voice again.")
			return false
		}
		_ = conn.Fail("stale", "Reload the page to use voice.")
		return false
	}
	return true
}

// finishVoiceTake transcribes the buffered audio and streams the result.
//
// Every ai.Provider call in this package must be preceded by
// aiMeter.Check in the same function (TestAIProviderCallsAreMetered), and
// that is exactly right here: transcription is the only respondent-facing
// operation that spends money, so the breaker has to see it.
func (s *server) finishVoiceTake(conn *ws.Conn, survey store.PublicSurvey, buf *voice.Buffer,
	session, language string) {
	ctx := conn.Context()
	seconds := buf.Seconds()
	if seconds == 0 {
		_ = conn.Fail("empty", "I didn't hear anything. Try again, or type your answer.")
		return
	}

	// Per-response budget: one respondent cannot spend a survey's whole
	// allowance on their own answers.
	if session != "" && s.voiceBudget.Remaining(session) < seconds {
		_ = conn.Fail("quota", voiceFallbackMessage)
		return
	}
	// Per-survey daily seconds, then the workspace quota and the global
	// breaker. All three refuse the same way: voice stops, typing stays.
	if left, err := s.aiMeter.VoiceSecondsLeft(ctx, survey.ID); err != nil {
		s.logger.Error("voice allowance lookup failed", "error", err)
		_ = conn.Fail("unavailable", voiceFallbackMessage)
		return
	} else if left < seconds {
		_ = conn.Fail("quota", voiceFallbackMessage)
		return
	}
	if err := s.aiMeter.Check(ctx, survey.WorkspaceID); err != nil {
		_ = conn.Fail("quota", voiceFallbackMessage)
		return
	}

	_ = conn.Status("Transcribing…")
	stream, err := s.ai.Transcribe(ctx, ai.TranscribeRequest{
		Audio:    bytes.NewReader(buf.WAV()),
		MIMEType: "audio/wav",
		Language: language,
	})
	if err != nil {
		s.logger.Error("transcription failed", "error", err)
		_ = conn.Fail("unavailable", voiceFallbackMessage)
		return
	}
	counted := ai.Counted(stream)
	defer counted.Close()

	// Charged whatever happens next: the audio was sent and the model
	// listened to it, so a stream that dies halfway still costs.
	defer func() {
		if session != "" {
			s.voiceBudget.Spend(session, seconds)
		}
		if err := s.aiMeter.RecordVoice(ctx, survey.WorkspaceID, &survey.ID, seconds, counted.Chars()); err != nil {
			s.logger.Error("recording voice usage failed", "error", err)
		}
	}()

	var transcript strings.Builder
	for {
		fragment, err := counted.Recv()
		if fragment != "" {
			transcript.WriteString(fragment)
			if sendErr := conn.Chunk(fragment); sendErr != nil {
				return // the respondent navigated away; stop spending
			}
		}
		if err != nil {
			if isStreamEnd(err) {
				break
			}
			s.logger.Error("transcription stream failed", "error", err)
			if transcript.Len() == 0 {
				_ = conn.Fail("unavailable", voiceFallbackMessage)
				return
			}
			// Partial transcript already delivered: let the respondent
			// keep and edit it rather than discarding their words.
			break
		}
	}
	_ = conn.Done()
}

// isStreamEnd distinguishes "the model finished" from "the stream broke".
func isStreamEnd(err error) bool { return errors.Is(err, io.EOF) }

// voiceFallbackMessage is story 39: exceeding a quota must read as a
// small inconvenience with an obvious way forward, not as a failure.
const voiceFallbackMessage = "Voice isn't available right now — please type your answer."

func (s *server) voiceAnswerSeconds() int {
	if s.cfg.VoiceMaxSecondsPerAnswer > 0 {
		return s.cfg.VoiceMaxSecondsPerAnswer
	}
	return 120
}

// voiceEnabled reports whether respondent pages should offer the mic at
// all: only when a transcriber is configured (Appendix D — an absent
// capability is an absent feature, never a broken button).
func (s *server) voiceEnabled() bool {
	return ai.Supports(s.ai, ai.OpTranscribe)
}
