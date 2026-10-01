package ai

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
)

// Vertex speaks the Vertex AI REST API directly — no vendor SDK. The
// wire format is a documented HTTP+SSE contract, and the codebase already
// has an SSE reader, an OAuth2 token source and a JSON encoder, so the
// whole integration is this file. That keeps PLAN.md Appendix F's
// minimal-dependency rule intact (one new indirect module, the GCE
// metadata client that ADC needs) and, more importantly, keeps Vertex
// one implementation of Provider among several rather than the shape
// the product is built around.
//
// Credentials come from Application Default Credentials: the attached
// service account on Cloud Run, `gcloud auth application-default login`
// on a developer machine. No key files, no secrets in config.
//
// ADR-0004 and ADR-0013 pin every call to EU processing; Location is
// configuration so that pin is visible in the environment rather than
// buried here. It is either a single region ("europe-west4") or one of
// the jurisdictional multi-regions ("eu", "us"), which live on a
// differently shaped host — see host.
type Vertex struct {
	Project  string
	Location string
	Models   ModelSet
	// Endpoint overrides the API host (tests point it at a stub server).
	// Empty means the Vertex host that serves Location.
	Endpoint string
	// FirstAnswerTimeout overrides how long one attempt may go without
	// beginning to answer (tests shorten it). Zero means the allowance
	// for the operation, see firstAnswerAllowance.
	FirstAnswerTimeout time.Duration
	// Client, when set, is used as-is; otherwise an ADC-authenticated
	// client is built on first use.
	Client *http.Client

	mu sync.Mutex
}

// maxInlineAudioBytes bounds what a single Transcribe call will read from
// the audio reader. Vertex rejects oversized inline payloads anyway; the
// point of the limit here is that a caller bug can never turn into
// unbounded memory in a process that promises to hold audio only in RAM
// (ADR-0004). M5's socket enforces a much tighter per-answer cap.
const maxInlineAudioBytes = 20 << 20

func (v *Vertex) httpClient(ctx context.Context) (*http.Client, error) {
	v.mu.Lock()
	defer v.mu.Unlock()
	if v.Client != nil {
		return v.Client, nil
	}
	// The token source outlives any single request, so it must not
	// capture a request context: a cancelled request would otherwise
	// break every later refresh.
	source, err := google.DefaultTokenSource(context.WithoutCancel(ctx),
		"https://www.googleapis.com/auth/cloud-platform")
	if err != nil {
		return nil, fmt.Errorf("ai: vertex credentials (application default credentials): %w", err)
	}
	v.Client = &http.Client{
		Transport: &oauth2.Transport{Source: source},
		Timeout:   5 * time.Minute,
	}
	return v.Client, nil
}

// host returns the API host for Location. Vertex has three host shapes:
// a single region is a subdomain prefix (europe-west4-aiplatform...),
// the jurisdictional multi-regions "eu" and "us" are served from
// aiplatform.<loc>.rep.googleapis.com, and the global endpoint (never
// used here, ADR-0011) is the bare aiplatform.googleapis.com. A
// multi-region name on the regional shape would resolve to nothing, so
// the distinction is made here rather than left to configuration.
func (v *Vertex) host() string {
	if v.Endpoint != "" {
		return strings.TrimSuffix(v.Endpoint, "/")
	}
	if isMultiRegion(v.Location) {
		return "https://aiplatform." + v.Location + ".rep.googleapis.com"
	}
	return "https://" + v.Location + "-aiplatform.googleapis.com"
}

// isMultiRegion reports whether a Vertex location is one of the
// jurisdictional multi-regions, whose endpoint guarantees ML processing
// inside that jurisdiction (for "eu": EU member states only).
func isMultiRegion(location string) bool {
	return location == "eu" || location == "us"
}

func (v *Vertex) Generate(ctx context.Context, req GenerateRequest) (Stream, error) {
	parts, err := vertexAttachmentParts(req.Attachments)
	if err != nil {
		return nil, err
	}
	return v.stream(ctx, OpGenerate, req.System, append(parts, vertexPart{Text: req.Prompt}))
}

// vertexInlineKinds are the attachment kinds Gemini reads natively as
// inline data. Office and OpenDocument files arrive here already turned
// into text by internal/attach, so plain text covers them.
var vertexInlineKinds = map[string]bool{
	MIMEText: true, MIMEPDF: true, MIMEPNG: true, MIMEJPEG: true,
}

// vertexAttachmentParts sends each attachment inline, after a line
// naming it, ahead of the prompt: the model reads the files first and
// the request about them last, and a prompt can say "the attached
// spreadsheet" because the model was told which file that is.
func vertexAttachmentParts(attachments []Attachment) ([]vertexPart, error) {
	var parts []vertexPart
	for _, a := range attachments {
		if !vertexInlineKinds[a.MIME] {
			return nil, fmt.Errorf("%w: %s (%s)", ErrAttachmentUnsupported, a.Name, a.MIME)
		}
		parts = append(parts,
			vertexPart{Text: "Attached file: " + a.Name},
			vertexPart{InlineData: &vertexBlob{MIMEType: a.MIME, Data: base64.StdEncoding.EncodeToString(a.Data)}},
		)
	}
	return parts, nil
}

func (v *Vertex) Analyze(ctx context.Context, req AnalyzeRequest) (Stream, error) {
	return v.stream(ctx, OpAnalyze, req.System, []vertexPart{{Text: req.Prompt}})
}

func (v *Vertex) Translate(ctx context.Context, req TranslateRequest) (Stream, error) {
	return v.stream(ctx, OpTranslate,
		translationSystemPrompt(req.SourceLang, req.TargetLang),
		[]vertexPart{{Text: req.Text}})
}

// Transcribe sends the audio inline and streams the transcript back. The
// audio exists as bytes in this process for the duration of one request
// and is never written anywhere (ADR-0004).
func (v *Vertex) Transcribe(ctx context.Context, req TranscribeRequest) (Stream, error) {
	audio, err := io.ReadAll(io.LimitReader(req.Audio, maxInlineAudioBytes+1))
	if err != nil {
		return nil, fmt.Errorf("ai: read audio: %w", err)
	}
	if len(audio) > maxInlineAudioBytes {
		return nil, fmt.Errorf("ai: audio exceeds %d bytes", maxInlineAudioBytes)
	}
	if len(audio) == 0 {
		return nil, fmt.Errorf("ai: no audio to transcribe")
	}
	mime := req.MIMEType
	if mime == "" {
		mime = "audio/wav"
	}
	parts := []vertexPart{
		{InlineData: &vertexBlob{MIMEType: mime, Data: base64.StdEncoding.EncodeToString(audio)}},
		{Text: transcriptionPrompt(req.Language)},
	}
	return v.stream(ctx, OpTranscribe, "", parts)
}

type vertexPart struct {
	Text       string      `json:"text,omitempty"`
	InlineData *vertexBlob `json:"inlineData,omitempty"`
}

type vertexBlob struct {
	MIMEType string `json:"mimeType"`
	Data     string `json:"data"`
}

func (v *Vertex) stream(ctx context.Context, op Op, system string, parts []vertexPart) (Stream, error) {
	model := v.Models.For(op)
	if model == "" || v.Project == "" || v.Location == "" {
		return nil, ErrUnsupported
	}

	body := map[string]any{
		"contents": []map[string]any{{"role": "user", "parts": parts}},
	}
	if system != "" {
		body["systemInstruction"] = map[string]any{
			"parts": []vertexPart{{Text: system}},
		}
	}
	if cfg := generationConfig(op, model); cfg != nil {
		body["generationConfig"] = cfg
	}
	payload, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("ai: encode vertex request: %w", err)
	}

	url := fmt.Sprintf("%s/v1/projects/%s/locations/%s/publishers/google/models/%s:streamGenerateContent?alt=sse",
		v.host(), v.Project, v.Location, model)

	allowance := v.FirstAnswerTimeout
	if allowance <= 0 {
		allowance = firstAnswerAllowance(op)
	}
	for attempt := 1; ; attempt++ {
		answer, err := v.attempt(ctx, op, url, payload, allowance)
		if err == nil {
			return answer, nil
		}
		if !errors.Is(err, errStalled) {
			return nil, err
		}
		if attempt == vertexAttempts {
			return nil, fmt.Errorf("ai: vertex %s request: %w in any of %d attempts", op, err, vertexAttempts)
		}
		slog.Default().Warn("ai: vertex did not begin to answer; asking again",
			"op", string(op), "attempt", attempt, "allowance", allowance.String())
	}
}

// A shared-capacity endpoint occasionally accepts a request and then
// says nothing for a minute or more, while the same request sent again
// is answered at the usual speed. Waiting such a call out is the worst
// option for the person watching, so an attempt that has not begun to
// answer within its allowance is abandoned and the request is made
// again. Only the beginning is timed: once the first fragment arrives
// the answer streams for as long as it needs.
const vertexAttempts = 3

// errStalled marks an attempt abandoned for saying nothing.
var errStalled = errors.New("the model did not begin to answer")

// firstAnswerAllowance is how long an attempt may take to produce its
// first fragment. The model thinks before it answers and sends nothing
// while it does, so the allowance has to cover the thinking an
// operation ordinarily needs with room to spare: a transcript asks for
// almost none, a summary of many responses for the most.
func firstAnswerAllowance(op Op) time.Duration {
	switch op {
	case OpTranscribe:
		return 20 * time.Second
	case OpAnalyze:
		return 90 * time.Second
	default:
		return 30 * time.Second
	}
}

// attempt makes the request once and returns a stream that has already
// produced its first fragment, or errStalled when the allowance ran out
// before it did.
func (v *Vertex) attempt(ctx context.Context, op Op, url string, payload []byte, allowance time.Duration) (Stream, error) {
	attemptCtx, cancel := context.WithCancel(ctx)
	var stalled atomic.Bool
	watchdog := time.AfterFunc(allowance, func() {
		stalled.Store(true)
		cancel()
	})
	fail := func(err error) (Stream, error) {
		watchdog.Stop()
		cancel()
		// The caller giving up is not a stall, whatever the timer says.
		if stalled.Load() && ctx.Err() == nil {
			return nil, errStalled
		}
		return nil, err
	}

	httpReq, err := http.NewRequestWithContext(attemptCtx, http.MethodPost, url, bytes.NewReader(payload))
	if err != nil {
		return fail(fmt.Errorf("ai: build vertex request: %w", err))
	}
	httpReq.Header.Set("Content-Type", "application/json")

	client, err := v.httpClient(ctx)
	if err != nil {
		return fail(err)
	}
	resp, err := client.Do(httpReq)
	if err != nil {
		return fail(fmt.Errorf("ai: vertex %s request: %w", op, err))
	}
	if resp.StatusCode >= 300 {
		// The body carries Google's error message, which names the real
		// problem (model id, region, permission) far better than the code.
		detail, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		resp.Body.Close()
		return fail(fmt.Errorf("ai: vertex %s request: status %d: %s", op, resp.StatusCode, detail))
	}

	events := newSSEStream(resp.Body, decodeVertexEvent)
	first, err := events.Recv()
	if err != nil && !errors.Is(err, io.EOF) {
		resp.Body.Close()
		return fail(err)
	}
	if !watchdog.Stop() || stalled.Load() {
		resp.Body.Close()
		return fail(errStalled)
	}
	return &startedStream{events: events, release: cancel, first: first, firstErr: err, unread: true}, nil
}

// startedStream is an answer whose first fragment has been read ahead,
// to prove the attempt alive. Recv hands that fragment over first.
type startedStream struct {
	events   *sseStream
	release  context.CancelFunc
	first    string
	firstErr error // io.EOF when the answer was empty
	unread   bool
}

func (s *startedStream) Recv() (string, error) {
	if s.unread {
		s.unread = false
		return s.first, s.firstErr
	}
	return s.events.Recv()
}

func (s *startedStream) Close() error {
	err := s.events.Close()
	s.release()
	return err
}

// generationConfig returns the request tuning for op on model, or nil
// when the model's family is not known to accept any. The plain request
// is always the fallback: a model id this table does not recognise gets
// the model's own defaults, never a parameter it might reject. Vertex
// answers an unknown field with 400 and no transcript, so a parameter
// that helps one family breaks voice outright on the next; a model
// switch therefore has to be a tfvars change and nothing else, and the
// only way to tune a new model is a row here, never a bare `if op ==`.
//
// Transcription asks for the least thinking the model allows: a
// transcript is the model's ears, not its judgement, and the respondent
// is watching the words arrive. Gemini 3.x reasons before it answers
// unless told otherwise, and takes thinkingLevel (LOW is the floor the
// Flash tier accepts). Gemini 2.5 has a different knob, thinkingBudget,
// and rejects thinkingLevel; it retires on 2026-10-20 and its default
// behaviour transcribed well enough, so it gets no row. The dedicated
// speech and live models (gemini-3.5-transcribe, gemini-3.8-live) are
// not documented to take thinking config, and the tier suffix keeps
// them out of the 3.x row. The other operations keep every model's
// default, where a moment's thought helps.
func generationConfig(op Op, model string) map[string]any {
	if op != OpTranscribe {
		return nil
	}
	if family := geminiFamily(model); family.major >= 3 {
		return map[string]any{
			"thinkingConfig": map[string]any{"thinkingLevel": "LOW"},
		}
	}
	return nil
}

// geminiFamilyPattern recognises the general-purpose Gemini tiers,
// "gemini-<major>.<minor>-flash" and "-pro" with any suffix after the
// tier (-lite, -preview). Dedicated models carry another tier word and
// deliberately do not match.
var geminiFamilyPattern = regexp.MustCompile(`^gemini-(\d+)\.(\d+)-(flash|pro)(-|$)`)

type geminiVersion struct{ major, minor int }

// geminiFamily reads the generation out of a general-purpose Gemini model
// id; the zero value means "not one of those".
func geminiFamily(model string) geminiVersion {
	m := geminiFamilyPattern.FindStringSubmatch(model)
	if m == nil {
		return geminiVersion{}
	}
	major, _ := strconv.Atoi(m[1])
	minor, _ := strconv.Atoi(m[2])
	return geminiVersion{major: major, minor: minor}
}

// decodeVertexEvent reads one streamGenerateContent frame. Vertex ends a
// stream by closing the body rather than sending a sentinel, so only real
// errors are surfaced here.
func decodeVertexEvent(data []byte) (string, error) {
	var event struct {
		Candidates []struct {
			Content struct {
				Parts []struct {
					Text string `json:"text"`
				} `json:"parts"`
			} `json:"content"`
		} `json:"candidates"`
		Error *struct {
			Message string `json:"message"`
			Status  string `json:"status"`
		} `json:"error"`
	}
	if err := json.Unmarshal(data, &event); err != nil {
		return "", nil // unknown frame shapes are skippable
	}
	if event.Error != nil {
		return "", fmt.Errorf("ai: vertex stream error: %s: %s", event.Error.Status, event.Error.Message)
	}
	var out strings.Builder
	for _, candidate := range event.Candidates {
		for _, part := range candidate.Content.Parts {
			out.WriteString(part.Text)
		}
	}
	return out.String(), nil
}

// Supports: whatever has a model configured, in a project and region.
func (v *Vertex) Supports(op Op) bool {
	return v.Project != "" && v.Location != "" && v.Models.For(op) != ""
}
