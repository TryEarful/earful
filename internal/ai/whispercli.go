package ai

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
)

// WhisperCLI transcribes by shelling out to whisper.cpp's whisper-cli —
// the local-dev transcription backend on this project's dev machine
// (`whisper-cli -m <model> -f <file>`). Transcription only; every other
// operation is ErrUnsupported and belongs to the text provider via
// Composite.
//
// ADR-0004 note: the audio must not outlive the request. The CLI needs a
// file, so the audio touches disk for the lifetime of one exec — in a
// private temp file, removed before this function returns, with the
// removal deferred so a crash mid-transcription cannot leak it past
// process death. This is the local-dev trade-off; the Vertex path streams
// from memory.
type WhisperCLI struct {
	// Bin is the whisper-cli executable; Model the ggml model path.
	Bin   string
	Model string
}

func (w *WhisperCLI) Generate(context.Context, GenerateRequest) (Stream, error) {
	return nil, ErrUnsupported
}

func (w *WhisperCLI) Translate(context.Context, TranslateRequest) (Stream, error) {
	return nil, ErrUnsupported
}

func (w *WhisperCLI) Analyze(context.Context, AnalyzeRequest) (Stream, error) {
	return nil, ErrUnsupported
}

func (w *WhisperCLI) Transcribe(ctx context.Context, req TranscribeRequest) (Stream, error) {
	tmp, err := os.CreateTemp("", "earful-voice-*"+extensionFor(req.MIMEType))
	if err != nil {
		return nil, fmt.Errorf("ai: temp audio file: %w", err)
	}
	defer os.Remove(tmp.Name()) //nolint:errcheck // best-effort cleanup either way
	if _, err := io.Copy(tmp, req.Audio); err != nil {
		tmp.Close()
		return nil, fmt.Errorf("ai: write audio: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return nil, fmt.Errorf("ai: close audio file: %w", err)
	}

	args := []string{"-m", w.Model, "-f", tmp.Name(), "--no-prints", "--no-timestamps"}
	if req.Language != "" {
		args = append(args, "--language", req.Language)
	}

	var stdout, stderr bytes.Buffer
	cmd := exec.CommandContext(ctx, w.Bin, args...)
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("ai: whisper-cli: %w: %s", err, truncate(stderr.String(), 300))
	}
	return newSliceStream(strings.TrimSpace(stdout.String())), nil
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
