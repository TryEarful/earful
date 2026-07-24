package ai_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/TryEarful/earful/internal/ai"
	"github.com/TryEarful/earful/internal/config"
)

// fakeOpenAIServer imitates an OpenAI-compatible backend: SSE for chat,
// JSON for transcriptions. It records the last request bodies for
// assertions.
func fakeOpenAIServer(t *testing.T) (*httptest.Server, *recorded) {
	t.Helper()
	rec := &recorded{}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /v1/chat/completions", func(w http.ResponseWriter, r *http.Request) {
		rec.auth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "text/event-stream")
		for _, fragment := range []string{"Hello", " from", " the", " model"} {
			fmt.Fprintf(w, "data: {\"choices\":[{\"delta\":{\"content\":\"%s\"}}]}\n\n", fragment)
		}
		fmt.Fprint(w, "data: [DONE]\n\n")
	})
	mux.HandleFunc("POST /v1/audio/transcriptions", func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseMultipartForm(1 << 20); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		rec.language = r.FormValue("language")
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"text":"transcribed words"}`)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv, rec
}

type recorded struct {
	auth     string
	language string
}

func TestOpenAICompat_StreamsChatFragments(t *testing.T) {
	t.Parallel()
	srv, rec := fakeOpenAIServer(t)
	provider := &ai.OpenAICompat{BaseURL: srv.URL + "/v1", Model: "test-model", APIKey: "sk-test"}

	stream, err := provider.Generate(context.Background(), ai.GenerateRequest{Prompt: "hi"})
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}

	// Fragment-by-fragment, not one buffered blob.
	first, err := stream.Recv()
	if err != nil || first != "Hello" {
		t.Fatalf("first fragment = %q (%v), want \"Hello\"", first, err)
	}
	rest, err := ai.Collect(stream)
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}
	if first+rest != "Hello from the model" {
		t.Errorf("full output = %q", first+rest)
	}
	if rec.auth != "Bearer sk-test" {
		t.Errorf("Authorization = %q", rec.auth)
	}
}

func TestOpenAICompat_TranscribePassesLanguageHint(t *testing.T) {
	t.Parallel()
	srv, rec := fakeOpenAIServer(t)
	provider := &ai.OpenAICompat{BaseURL: srv.URL + "/v1", Model: "whisper-test", SupportsAudio: true}

	stream, err := provider.Transcribe(context.Background(), ai.TranscribeRequest{
		Audio:    strings.NewReader("fake-audio-bytes"),
		MIMEType: "audio/webm",
		Language: "nl",
	})
	if err != nil {
		t.Fatalf("Transcribe: %v", err)
	}
	text, err := ai.Collect(stream)
	if err != nil || text != "transcribed words" {
		t.Fatalf("transcript = %q (%v)", text, err)
	}
	// The respondent's language choice must reach the backend (M11-T3).
	if rec.language != "nl" {
		t.Errorf("language hint = %q, want nl", rec.language)
	}
}

func TestOpenAICompat_TranscribeWithoutAudioSupport(t *testing.T) {
	t.Parallel()
	provider := &ai.OpenAICompat{BaseURL: "http://unused", Model: "m"}
	_, err := provider.Transcribe(context.Background(), ai.TranscribeRequest{Audio: strings.NewReader("x")})
	if err != ai.ErrUnsupported {
		t.Errorf("err = %v, want ErrUnsupported", err)
	}
}

// TestWhisperCLI_TranscribesViaExec runs a stub whisper-cli (a shell
// script) and checks argument passing — model, file, language hint — and
// output capture.
func TestWhisperCLI_TranscribesViaExec(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("stub script is a shell script")
	}
	dir := t.TempDir()
	stub := filepath.Join(dir, "whisper-stub")
	script := `#!/bin/sh
# echo the args to a file so the test can assert on them, then emit a transcript
echo "$@" > "$(dirname "$0")/args.txt"
echo " This is the transcript. "
`
	if err := os.WriteFile(stub, []byte(script), 0o755); err != nil {
		t.Fatalf("write stub: %v", err)
	}

	provider := &ai.WhisperCLI{Bin: stub, Model: "/models/test.bin"}
	stream, err := provider.Transcribe(context.Background(), ai.TranscribeRequest{
		Audio:    strings.NewReader("RIFF-fake-wav"),
		MIMEType: "audio/wav",
		Language: "en",
	})
	if err != nil {
		t.Fatalf("Transcribe: %v", err)
	}
	text, err := ai.Collect(stream)
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}
	if text != "This is the transcript." {
		t.Errorf("transcript = %q (whitespace should be trimmed)", text)
	}

	args, err := os.ReadFile(filepath.Join(dir, "args.txt"))
	if err != nil {
		t.Fatalf("read args: %v", err)
	}
	for _, want := range []string{"-m /models/test.bin", "-f ", "--language en", "--no-prints"} {
		if !strings.Contains(string(args), want) {
			t.Errorf("whisper-cli args missing %q: %s", want, args)
		}
	}

	// The temp audio file must be gone (ADR-0004: audio never persists).
	argFields := strings.Fields(string(args))
	for i, field := range argFields {
		if field == "-f" && i+1 < len(argFields) {
			if _, err := os.Stat(argFields[i+1]); !os.IsNotExist(err) {
				t.Errorf("audio temp file still exists after transcription: %s", argFields[i+1])
			}
		}
	}
}

func TestWhisperCLI_TextOperationsUnsupported(t *testing.T) {
	t.Parallel()
	provider := &ai.WhisperCLI{Bin: "x", Model: "y"}
	if _, err := provider.Generate(context.Background(), ai.GenerateRequest{}); err != ai.ErrUnsupported {
		t.Errorf("Generate err = %v, want ErrUnsupported", err)
	}
	if _, err := provider.Translate(context.Background(), ai.TranslateRequest{}); err != ai.ErrUnsupported {
		t.Errorf("Translate err = %v, want ErrUnsupported", err)
	}
	if _, err := provider.Analyze(context.Background(), ai.AnalyzeRequest{}); err != ai.ErrUnsupported {
		t.Errorf("Analyze err = %v, want ErrUnsupported", err)
	}
}

func TestComposite_RoutesAndDegrades(t *testing.T) {
	t.Parallel()
	text := &ai.Fake{GenerateScript: [][]string{{"generated"}}}
	voice := &ai.Fake{TranscribeScript: [][]string{{"heard"}}}
	full := &ai.Composite{Text: text, Transcriber: voice}

	stream, err := full.Generate(context.Background(), ai.GenerateRequest{Prompt: "p"})
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if out, _ := ai.Collect(stream); out != "generated" {
		t.Errorf("Generate routed wrong: %q", out)
	}
	stream, err = full.Transcribe(context.Background(), ai.TranscribeRequest{Audio: strings.NewReader("a")})
	if err != nil {
		t.Fatalf("Transcribe: %v", err)
	}
	if out, _ := ai.Collect(stream); out != "heard" {
		t.Errorf("Transcribe routed wrong: %q", out)
	}
	if len(text.TranscribeCalls) != 0 || len(voice.GenerateCalls) != 0 {
		t.Error("operations leaked across the composite split")
	}

	// Absent capabilities degrade, not crash.
	textOnly := &ai.Composite{Text: text}
	if _, err := textOnly.Transcribe(context.Background(), ai.TranscribeRequest{Audio: strings.NewReader("a")}); err != ai.ErrUnsupported {
		t.Errorf("missing transcriber err = %v, want ErrUnsupported", err)
	}
	nothing := &ai.Composite{}
	if _, err := nothing.Generate(context.Background(), ai.GenerateRequest{}); err != ai.ErrUnsupported {
		t.Errorf("missing text err = %v, want ErrUnsupported", err)
	}
}

func TestFromConfig_SwapsViaEnv(t *testing.T) {
	t.Parallel()
	none := ai.FromConfig(config.Config{AIProvider: "none", TranscribeProvider: "none"})
	if _, err := none.Generate(context.Background(), ai.GenerateRequest{}); err != ai.ErrUnsupported {
		t.Errorf("none provider should be unsupported, got %v", err)
	}

	// openai text + whisper-cli voice — the local-dev shape. Construction
	// alone proves the wiring; behavior is covered above.
	local := ai.FromConfig(config.Config{
		AIProvider: "openai", AIBaseURL: "http://localhost:11434/v1", AIModel: "m",
		TranscribeProvider: "whisper-cli", WhisperBin: "whisper-cli", WhisperModel: "/m.bin",
	})
	if local == nil {
		t.Fatal("FromConfig returned nil")
	}
}

// TestOpenAICompat_Integration runs against a real OpenAI-compatible
// backend when AI_TEST_BASE_URL and AI_TEST_MODEL are set (ollama:
// http://localhost:11434/v1; llamafile: http://localhost:8081/v1) —
// M6-T1's integration AC, run on demand. Skipped otherwise.
func TestOpenAICompat_Integration(t *testing.T) {
	baseURL := os.Getenv("AI_TEST_BASE_URL")
	model := os.Getenv("AI_TEST_MODEL")
	if baseURL == "" || model == "" {
		t.Skip("AI_TEST_BASE_URL / AI_TEST_MODEL not set; see docs/testing.md")
	}
	provider := &ai.OpenAICompat{BaseURL: baseURL, Model: model}
	stream, err := provider.Generate(context.Background(), ai.GenerateRequest{
		System: "Answer with exactly one word.",
		Prompt: "What is the capital of France?",
	})
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	out, err := ai.Collect(stream)
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}
	if !strings.Contains(strings.ToLower(out), "paris") {
		t.Errorf("model output %q does not mention paris", out)
	}
	t.Logf("model said: %q", out)
}
