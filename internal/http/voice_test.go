package http_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/TryEarful/earful/internal/ai"
	"github.com/TryEarful/earful/internal/apptest"
	"github.com/TryEarful/earful/internal/voice"
)

// Voice is driven here exactly as the browser drives it: open the socket
// the rendered page points at, send the start frame it would send, push
// PCM, stop, and read the frames back. Nothing reaches inside the app.

type voiceFrame struct {
	Type  string `json:"type"`
	Text  string `json:"text"`
	Error string `json:"error"`
	Code  string `json:"code"`
}

// dialVoice opens the voice socket for a rendered respondent page and
// sends the start frame, carrying the same form token and nonce the page
// itself carries.
func dialVoice(t *testing.T, app *apptest.App, path, page string) *websocket.Conn {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	wsURL := "ws" + strings.TrimPrefix(app.Server.URL, "http") + path
	conn, _, err := websocket.Dial(ctx, wsURL, &websocket.DialOptions{
		HTTPHeader: http.Header{"Origin": {app.Server.URL}},
	})
	if err != nil {
		t.Fatalf("dial %s: %v", path, err)
	}
	t.Cleanup(func() { conn.CloseNow() })

	form := respondForm(t, page)
	// The browser sends what the page declares (document.documentElement
	// .lang), which is how a respondent's chosen language reaches the
	// transcriber (M11-T3).
	start, _ := json.Marshal(map[string]any{
		"action": "start",
		"params": map[string]string{
			"token": form.Get("form_ts"),
			"nonce": form.Get("form_nonce"),
			"lang":  pageLanguage(page),
		},
	})
	if err := conn.Write(context.Background(), websocket.MessageText, start); err != nil {
		t.Fatalf("send start: %v", err)
	}
	return conn
}

var htmlLangRe = regexp.MustCompile(`<html lang="([^"]+)"`)

// pageLanguage reads what the page declares itself to be.
func pageLanguage(page string) string {
	if m := htmlLangRe.FindStringSubmatch(page); m != nil {
		return m[1]
	}
	return ""
}

// speak sends seconds of (silent) PCM and stops the take.
func speak(t *testing.T, conn *websocket.Conn, seconds int) {
	t.Helper()
	ctx := context.Background()
	chunk := make([]byte, voice.BytesPerSecond/4) // quarter-second frames
	for i := 0; i < seconds*4; i++ {
		if err := conn.Write(ctx, websocket.MessageBinary, chunk); err != nil {
			t.Fatalf("send audio: %v", err)
		}
	}
	if err := conn.Write(ctx, websocket.MessageText, []byte(`{"action":"stop"}`)); err != nil {
		t.Fatalf("send stop: %v", err)
	}
}

// readVoice collects frames until done or error.
func readVoice(t *testing.T, conn *websocket.Conn) (transcript string, last voiceFrame) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	var out strings.Builder
	for {
		_, data, err := conn.Read(ctx)
		if err != nil {
			t.Fatalf("read frame: %v (transcript so far %q)", err, out.String())
		}
		var frame voiceFrame
		if err := json.Unmarshal(data, &frame); err != nil {
			t.Fatalf("decode frame %q: %v", data, err)
		}
		switch frame.Type {
		case "chunk":
			out.WriteString(frame.Text)
		case "done", "error":
			return out.String(), frame
		}
	}
}

// TestVoice_SpokenAnswerBecomesAnEditableTranscript is stories 33, 35 and
// 36 end to end: speech goes up, a transcript comes back in fragments,
// and it is the respondent — not the model — who submits the answer.
func TestVoice_SpokenAnswerBecomesAnEditableTranscript(t *testing.T) {
	t.Parallel()
	fake := &ai.Fake{TranscribeScript: [][]string{{"I loved ", "the onboarding."}}}
	app := apptest.New(t, apptest.Options{AI: fake})
	creator := app.Login(t, apptest.UniqueEmail("voice"))
	id := publishedSurvey(t, app, creator, "Voice survey", true,
		[3]string{"long_text", "How did it go?", ""},
	)

	respondent := &http.Client{}
	page := mustGet(t, respondent, app.Server.URL+"/s/"+id)
	if !strings.Contains(page, `data-voice-path="/s/`+id+`/voice"`) {
		t.Fatalf("the page did not offer voice:\n%s", page)
	}

	conn := dialVoice(t, app, "/s/"+id+"/voice", page)
	speak(t, conn, 2)
	transcript, last := readVoice(t, conn)

	if last.Type != "done" {
		t.Fatalf("last frame = %+v, want done", last)
	}
	if transcript != "I loved the onboarding." {
		t.Errorf("transcript = %q", transcript)
	}
	if len(fake.TranscribeCalls) != 1 {
		t.Fatalf("provider calls = %d, want 1", len(fake.TranscribeCalls))
	}
	call := fake.TranscribeCalls[0]
	// The page's language reaches the model. On an untranslated survey
	// that is "en"; M11-T3's test covers a respondent who chose another.
	if call.Language != "en" {
		t.Errorf("language hint = %q, want the page's language", call.Language)
	}
	// What arrives is a well-formed WAV of the audio that was sent, not a
	// file path or a reference to storage.
	if string(call.Audio[0:4]) != "RIFF" {
		t.Errorf("provider received %q..., want a WAV", call.Audio[:8])
	}
	if len(call.Audio) < voice.BytesPerSecond {
		t.Errorf("provider received %d bytes, less than the audio sent", len(call.Audio))
	}

	// The transcript is not an answer until the respondent submits it:
	// the survey has no responses at this point.
	if body := app.SurveyPage(t, creator, id); bodyContains(body, "1 response") {
		t.Error("transcribing created a response on its own")
	}

	// And submitting the edited text is an ordinary form post.
	form := respondForm(t, page)
	identity := extractAnswerFields(t, page)[0]
	form.Set("q_"+identity, "I loved the onboarding, though setup took a while.")
	_, body := submitAfterReading(t, app, respondent, id, form)
	if !bodyContains(body, "Thank you") {
		t.Errorf("the edited transcript was not accepted:\n%s", body)
	}
}

// TestVoice_IsAbsentWhenNoTranscriberIsConfigured is Appendix D: a
// capability that is not configured is not offered. Every pre-M5 test
// runs in exactly this state, which is the regression proof.
func TestVoice_IsAbsentWhenNoTranscriberIsConfigured(t *testing.T) {
	t.Parallel()
	app := apptest.New(t, apptest.Options{}) // no AI at all
	creator := app.Login(t, apptest.UniqueEmail("novoice"))
	id := publishedSurvey(t, app, creator, "No voice", true,
		[3]string{"long_text", "How did it go?", ""},
	)

	respondent := &http.Client{}
	page := mustGet(t, respondent, app.Server.URL+"/s/"+id)
	if strings.Contains(page, "data-voice-path") || strings.Contains(page, "voice.js") {
		t.Errorf("voice was offered with no transcriber configured:\n%s", page)
	}

	// And the endpoint refuses rather than half-working.
	resp, err := http.Get(app.Server.URL + "/s/" + id + "/voice")
	if err != nil {
		t.Fatalf("GET voice socket: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusSwitchingProtocols || resp.StatusCode == http.StatusOK {
		t.Errorf("voice socket status = %d, want a refusal", resp.StatusCode)
	}
}

// TestVoice_RefusesWithoutAValidRenderToken: the socket is reachable only
// from a page we served, the same rule the form itself follows.
func TestVoice_RefusesWithoutAValidRenderToken(t *testing.T) {
	t.Parallel()
	fake := &ai.Fake{TranscribeScript: [][]string{{"should not happen"}}}
	app := apptest.New(t, apptest.Options{AI: fake})
	creator := app.Login(t, apptest.UniqueEmail("voicetoken"))
	id := publishedSurvey(t, app, creator, "Token", true,
		[3]string{"long_text", "How did it go?", ""},
	)

	ctx := context.Background()
	wsURL := "ws" + strings.TrimPrefix(app.Server.URL, "http") + "/s/" + id + "/voice"
	conn, _, err := websocket.Dial(ctx, wsURL, &websocket.DialOptions{
		HTTPHeader: http.Header{"Origin": {app.Server.URL}},
	})
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.CloseNow()

	forged, _ := json.Marshal(map[string]any{
		"action": "start",
		"params": map[string]string{"token": "1700000000.not-a-real-signature"},
	})
	if err := conn.Write(ctx, websocket.MessageText, forged); err != nil {
		t.Fatalf("send start: %v", err)
	}
	_, frame := readVoice(t, conn)
	if frame.Type != "error" {
		t.Errorf("frame = %+v, want an error", frame)
	}
	if len(fake.TranscribeCalls) != 0 {
		t.Error("a forged token reached the transcription provider")
	}
}

// TestVoice_QuotaDegradesToTyping is story 39: running out of voice
// allowance must read as an inconvenience with an obvious way forward.
func TestVoice_QuotaDegradesToTyping(t *testing.T) {
	t.Parallel()
	fake := &ai.Fake{TranscribeScript: [][]string{{"first take"}}}
	app := apptest.New(t, apptest.Options{
		AI: fake,
		// One workspace token: enough for nothing, so the workspace quota
		// trips on the first attempt.
		AIQuota: 1,
	})
	creator := app.Login(t, apptest.UniqueEmail("voicequota"))
	id := publishedSurvey(t, app, creator, "Quota", true,
		[3]string{"long_text", "How did it go?", ""},
	)

	respondent := &http.Client{}
	page := mustGet(t, respondent, app.Server.URL+"/s/"+id)

	// Burn the workspace's allowance through an ordinary, metered path.
	conn := dialVoice(t, app, "/s/"+id+"/voice", page)
	speak(t, conn, 1)
	if _, frame := readVoice(t, conn); frame.Type == "error" && frame.Code != "quota" {
		t.Fatalf("unexpected first-take error: %+v", frame)
	}

	page = mustGet(t, respondent, app.Server.URL+"/s/"+id)
	conn = dialVoice(t, app, "/s/"+id+"/voice", page)
	speak(t, conn, 1)
	_, frame := readVoice(t, conn)
	if frame.Type != "error" || frame.Code != "quota" {
		t.Fatalf("second take = %+v, want a quota refusal", frame)
	}
	if !strings.Contains(frame.Error, "type your answer") {
		t.Errorf("quota message %q does not point at typing (story 39)", frame.Error)
	}

	// Typing still works, which is the whole point.
	form := respondForm(t, page)
	form.Set("q_"+extractAnswerFields(t, page)[0], "Typed instead.")
	_, body := submitAfterReading(t, app, respondent, id, form)
	if !bodyContains(body, "Thank you") {
		t.Errorf("typing was blocked by a voice quota:\n%s", body)
	}
}

// TestVoice_InvitedSurveysSpeakThroughTheirPersonalLink: the token is the
// credential there, and the public socket must not serve an invited
// survey at all.
func TestVoice_InvitedSurveysSpeakThroughTheirPersonalLink(t *testing.T) {
	t.Parallel()
	fake := &ai.Fake{TranscribeScript: [][]string{{"spoken by an invitee"}}}
	app := apptest.New(t, apptest.Options{AI: fake})
	creator := app.Login(t, apptest.UniqueEmail("voiceinvited"))
	id := app.CreateSurvey(t, creator, "Invited voice", false)
	app.AddQuestion(t, creator, id, "long_text", "How did it go?", nil)
	app.Publish(t, creator, id)

	invitee := apptest.UniqueEmail("guest")
	app.PostForm(t, creator, "/surveys/"+id+"/participants", url.Values{"emails": {invitee}}).Body.Close()
	app.PostForm(t, creator, "/surveys/"+id+"/participants/send", nil).Body.Close()
	link := inviteLinkTo(t, app, invitee)

	respondent := &http.Client{}
	page := mustGet(t, respondent, link)
	token := strings.TrimPrefix(link, app.Server.URL+"/p/")
	if !strings.Contains(page, `data-voice-path="/p/`+token+`/voice"`) {
		t.Fatalf("the personal link did not offer voice:\n%s", page)
	}

	conn := dialVoice(t, app, "/p/"+token+"/voice", page)
	speak(t, conn, 1)
	transcript, frame := readVoice(t, conn)
	if frame.Type != "done" || transcript != "spoken by an invitee" {
		t.Errorf("transcript = %q, last frame %+v", transcript, frame)
	}

	// The public socket still refuses an invited survey.
	resp, err := http.Get(app.Server.URL + "/s/" + id + "/voice")
	if err != nil {
		t.Fatalf("GET public voice socket: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("public voice socket on an invited survey = %d, want 404", resp.StatusCode)
	}
}
