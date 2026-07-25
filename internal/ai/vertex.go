package ai

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
)

// Vertex speaks the Vertex AI REST API directly — no vendor SDK. The
// wire format is a documented HTTP+SSE contract and we already have an
// SSE reader, an OAuth2 token source and a JSON encoder, so the whole
// integration is this file. That keeps PLAN.md Appendix F's
// minimal-dependency rule intact (one new indirect module, the GCE
// metadata client that ADC needs) and, more importantly, keeps Vertex
// one implementation of Provider among several rather than the shape
// the product is built around.
//
// Credentials come from Application Default Credentials: the attached
// service account on Cloud Run, `gcloud auth application-default login`
// on a developer machine. No key files, no secrets in config.
//
// ADR-0004 pins transcription to europe-west4; Location is configuration
// so that pin is visible in the environment rather than buried here.
type Vertex struct {
	Project  string
	Location string
	Models   ModelSet
	// Endpoint overrides the API host (tests point it at a stub server).
	// Empty means the regional Vertex host for Location.
	Endpoint string
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

func (v *Vertex) host() string {
	if v.Endpoint != "" {
		return strings.TrimSuffix(v.Endpoint, "/")
	}
	return "https://" + v.Location + "-aiplatform.googleapis.com"
}

func (v *Vertex) Generate(ctx context.Context, req GenerateRequest) (Stream, error) {
	return v.stream(ctx, OpGenerate, req.System, []vertexPart{{Text: req.Prompt}})
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
	payload, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("ai: encode vertex request: %w", err)
	}

	url := fmt.Sprintf("%s/v1/projects/%s/locations/%s/publishers/google/models/%s:streamGenerateContent?alt=sse",
		v.host(), v.Project, v.Location, model)
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(payload))
	if err != nil {
		return nil, fmt.Errorf("ai: build vertex request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	client, err := v.httpClient(ctx)
	if err != nil {
		return nil, err
	}
	resp, err := client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("ai: vertex %s request: %w", op, err)
	}
	if resp.StatusCode >= 300 {
		// The body carries Google's error message, which names the real
		// problem (model id, region, permission) far better than the code.
		detail, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		resp.Body.Close()
		return nil, fmt.Errorf("ai: vertex %s request: status %d: %s", op, resp.StatusCode, detail)
	}
	return newSSEStream(resp.Body, decodeVertexEvent), nil
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
