package ai

import (
	"bufio"
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
	Model   string
	APIKey  string // optional; local backends ignore it
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
	return o.chat(ctx, req.System, req.Prompt)
}

func (o *OpenAICompat) Analyze(ctx context.Context, req AnalyzeRequest) (Stream, error) {
	return o.chat(ctx, req.System, req.Prompt)
}

func (o *OpenAICompat) Translate(ctx context.Context, req TranslateRequest) (Stream, error) {
	system := fmt.Sprintf(
		"You are a translator. Translate the user's text from %s to %s. Output only the translation — no preamble, no notes.",
		orAuto(req.SourceLang), req.TargetLang)
	return o.chat(ctx, system, req.Text)
}

func orAuto(lang string) string {
	if lang == "" {
		return "the source language (detect it)"
	}
	return lang
}

// chat starts a streaming chat completion and returns the SSE stream.
func (o *OpenAICompat) chat(ctx context.Context, system, prompt string) (Stream, error) {
	messages := []map[string]string{}
	if system != "" {
		messages = append(messages, map[string]string{"role": "system", "content": system})
	}
	messages = append(messages, map[string]string{"role": "user", "content": prompt})

	payload, err := json.Marshal(map[string]any{
		"model":    o.Model,
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
	return &sseStream{body: resp.Body, scanner: bufio.NewScanner(resp.Body)}, nil
}

// sseStream parses OpenAI-style server-sent events into text fragments.
type sseStream struct {
	body    io.ReadCloser
	scanner *bufio.Scanner
}

func (s *sseStream) Recv() (string, error) {
	for s.scanner.Scan() {
		line := strings.TrimSpace(s.scanner.Text())
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if data == "[DONE]" {
			return "", io.EOF
		}
		var event struct {
			Choices []struct {
				Delta struct {
					Content string `json:"content"`
				} `json:"delta"`
			} `json:"choices"`
		}
		if err := json.Unmarshal([]byte(data), &event); err != nil {
			continue // keep-alives and unknown event shapes are skippable
		}
		if len(event.Choices) > 0 && event.Choices[0].Delta.Content != "" {
			return event.Choices[0].Delta.Content, nil
		}
	}
	if err := s.scanner.Err(); err != nil {
		return "", fmt.Errorf("ai: read stream: %w", err)
	}
	return "", io.EOF
}

func (s *sseStream) Close() error { return s.body.Close() }

// Transcribe posts multipart audio to the whisper-style endpoint. The
// response arrives whole (these endpoints don't stream), so it is
// returned as a single-fragment stream.
func (o *OpenAICompat) Transcribe(ctx context.Context, req TranscribeRequest) (Stream, error) {
	if !o.SupportsAudio {
		return nil, ErrUnsupported
	}

	var buf bytes.Buffer
	form := newAudioForm(&buf)
	if err := form.write(req, o.Model); err != nil {
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
