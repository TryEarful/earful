package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// OpenAICompat speaks the OpenAI-compatible chat API that ollama
// (`/v1`), llamafile and countless self-hosted gateways expose (PLAN.md
// Appendix F). Text operations stream over SSE; Transcribe posts to the
// whisper-style audio endpoint when the backend has one, else reports
// ErrUnsupported (local dev pairs this with the whisper-cli provider via
// Composite).
type OpenAICompat struct {
	// BaseURL up to and including the version prefix, e.g.
	// "http://localhost:11434/v1" (ollama) or "http://localhost:8081/v1"
	// (llamafile).
	BaseURL string
	// Models is the model per operation; most local setups serve one
	// model and set only Default.
	Models ModelSet
	APIKey string // optional; local backends ignore it
	// Client defaults to a client with a generous timeout — model
	// inference is slow by web standards.
	Client *http.Client
	// SupportsAudio enables the /audio/transcriptions endpoint; most
	// local text backends lack it.
	SupportsAudio bool
}

func (o *OpenAICompat) client() *http.Client {
	if o.Client != nil {
		return o.Client
	}
	return &http.Client{Timeout: 5 * time.Minute}
}

func (o *OpenAICompat) Generate(ctx context.Context, req GenerateRequest) (Stream, error) {
	return o.chat(ctx, OpGenerate, req.System, req.Prompt)
}

func (o *OpenAICompat) Analyze(ctx context.Context, req AnalyzeRequest) (Stream, error) {
	return o.chat(ctx, OpAnalyze, req.System, req.Prompt)
}

func (o *OpenAICompat) Translate(ctx context.Context, req TranslateRequest) (Stream, error) {
	return o.chat(ctx, OpTranslate, translationSystemPrompt(req.SourceLang, req.TargetLang), req.Text)
}

// chat starts a streaming chat completion and returns the SSE stream.
func (o *OpenAICompat) chat(ctx context.Context, op Op, system, prompt string) (Stream, error) {
	messages := []map[string]string{}
	if system != "" {
		messages = append(messages, map[string]string{"role": "system", "content": system})
	}
	messages = append(messages, map[string]string{"role": "user", "content": prompt})

	payload, err := json.Marshal(map[string]any{
		"model":    o.Models.For(op),
		"messages": messages,
		"stream":   true,
	})
	if err != nil {
		return nil, fmt.Errorf("ai: encode chat request: %w", err)
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost,
		strings.TrimSuffix(o.BaseURL, "/")+"/chat/completions", bytes.NewReader(payload))
	if err != nil {
		return nil, fmt.Errorf("ai: build chat request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	if o.APIKey != "" {
		httpReq.Header.Set("Authorization", "Bearer "+o.APIKey)
	}

	resp, err := o.client().Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("ai: chat request: %w", err)
	}
	if resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		resp.Body.Close()
		return nil, fmt.Errorf("ai: chat request: status %d: %s", resp.StatusCode, body)
	}
	return newSSEStream(resp.Body, decodeOpenAIEvent), nil
}

// decodeOpenAIEvent reads one chat-completion delta.
func decodeOpenAIEvent(data []byte) (string, error) {
	if string(data) == "[DONE]" {
		return "", io.EOF
	}
	var event struct {
		Choices []struct {
			Delta struct {
				Content string `json:"content"`
			} `json:"delta"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(data, &event); err != nil {
		return "", nil // keep-alives and unknown event shapes are skippable
	}
	if len(event.Choices) > 0 {
		return event.Choices[0].Delta.Content, nil
	}
	return "", nil
}

// Transcribe posts multipart audio to the whisper-style endpoint. The
// response arrives whole (these endpoints don't stream), so it is
// returned as a single-fragment stream.
func (o *OpenAICompat) Transcribe(ctx context.Context, req TranscribeRequest) (Stream, error) {
	if !o.SupportsAudio {
		return nil, ErrUnsupported
	}

	var buf bytes.Buffer
	form := newAudioForm(&buf)
	if err := form.write(req, o.Models.For(OpTranscribe)); err != nil {
		return nil, err
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost,
		strings.TrimSuffix(o.BaseURL, "/")+"/audio/transcriptions", &buf)
	if err != nil {
		return nil, fmt.Errorf("ai: build transcribe request: %w", err)
	}
	httpReq.Header.Set("Content-Type", form.contentType())
	if o.APIKey != "" {
		httpReq.Header.Set("Authorization", "Bearer "+o.APIKey)
	}

	resp, err := o.client().Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("ai: transcribe request: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return nil, fmt.Errorf("ai: transcribe request: status %d: %s", resp.StatusCode, body)
	}
	var result struct {
		Text string `json:"text"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("ai: decode transcription: %w", err)
	}
	return newSliceStream(result.Text), nil
}

// Supports: the chat endpoint covers every text operation; audio needs
// the whisper-style endpoint, which most local backends lack.
func (o *OpenAICompat) Supports(op Op) bool {
	if op == OpTranscribe {
		return o.SupportsAudio
	}
	return true
}
