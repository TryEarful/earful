package ai_test

import (
	"context"
	"encoding/json"
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
	provider := &ai.OpenAICompat{BaseURL: srv.URL + "/v1", Models: ai.ModelSet{Default: "test-model"}, APIKey: "sk-test"}

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
	provider := &ai.OpenAICompat{BaseURL: srv.URL + "/v1", Models: ai.ModelSet{Default: "whisper-test"}, SupportsAudio: true}

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
	provider := &ai.OpenAICompat{BaseURL: "http://unused", Models: ai.ModelSet{Default: "m"}}
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

	// Text configured but voice left off must not accidentally acquire a
	// transcriber, and vice versa — that split is how a self-hoster runs
	// text AI with no voice at all (Appendix D).
	textOnly := ai.FromConfig(config.Config{
		AIProvider: "openai", AIBaseURL: "http://x/v1", AIModel: "m", TranscribeProvider: "none",
	})
	if _, err := textOnly.Transcribe(context.Background(), ai.TranscribeRequest{Audio: strings.NewReader("a")}); err != ai.ErrUnsupported {
		t.Errorf("transcribe with TRANSCRIBE_PROVIDER=none: err = %v, want ErrUnsupported", err)
	}
	// Asking for the OpenAI audio endpoint without an OpenAI text backend
	// cannot silently half-work.
	orphan := ai.FromConfig(config.Config{AIProvider: "none", TranscribeProvider: "openai"})
	if _, err := orphan.Transcribe(context.Background(), ai.TranscribeRequest{Audio: strings.NewReader("a")}); err != ai.ErrUnsupported {
		t.Errorf("orphaned openai transcriber: err = %v, want ErrUnsupported", err)
	}

	// Vertex serves both halves from one client when both are pointed at
	// it, and the per-operation models reach the provider.
	vertex := ai.FromConfig(config.Config{
		AIProvider: "vertex", VertexProject: "earful-stg", VertexLocation: "europe-west4",
		AIModel: "flash", AIModelAnalyze: "pro", TranscribeProvider: "vertex",
	})
	if vertex == nil {
		t.Fatal("FromConfig returned nil for vertex")
	}
}

func TestModelSet_PerOperationOverrides(t *testing.T) {
	t.Parallel()
	models := ai.ModelSet{Default: "flash", Analyze: "pro"}
	if got := models.For(ai.OpAnalyze); got != "pro" {
		t.Errorf("analyze model = %q, want pro", got)
	}
	for _, op := range []ai.Op{ai.OpGenerate, ai.OpTranslate, ai.OpTranscribe} {
		if got := models.For(op); got != "flash" {
			t.Errorf("%s model = %q, want the default", op, got)
		}
	}
	if !(ai.ModelSet{}).Empty() || (ai.ModelSet{Translate: "x"}).Empty() {
		t.Error("Empty() misreports whether any model is configured")
	}
}

func TestScripted_ProducesUsableOutputForEveryOperation(t *testing.T) {
	t.Parallel()
	provider := &ai.Scripted{}

	stream, err := provider.Generate(context.Background(), ai.GenerateRequest{Prompt: "customer onboarding"})
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	generated, err := ai.Collect(stream)
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}
	// One JSON object per line, so a reader can surface each question as
	// it completes (M6-T3).
	for _, line := range strings.Split(strings.TrimSpace(generated), "\n") {
		var q struct {
			Type string `json:"type"`
			Text string `json:"text"`
		}
		if err := json.Unmarshal([]byte(line), &q); err != nil {
			t.Fatalf("scripted generation is not NDJSON: %q: %v", line, err)
		}
		if q.Type == "" || q.Text == "" {
			t.Errorf("scripted question missing type or text: %q", line)
		}
	}

	stream, err = provider.Translate(context.Background(), ai.TranslateRequest{Text: "How was it?", TargetLang: "nl"})
	if err != nil {
		t.Fatalf("Translate: %v", err)
	}
	translated, err := ai.Collect(stream)
	if err != nil || !strings.Contains(translated, "[nl]") {
		t.Errorf("scripted translation = %q (%v); it must be visibly scripted", translated, err)
	}

	stream, err = provider.Transcribe(context.Background(),
		ai.TranscribeRequest{Audio: strings.NewReader("audio"), MIMEType: "audio/wav"})
	if err != nil {
		t.Fatalf("Transcribe: %v", err)
	}
	transcript, err := ai.Collect(stream)
	if err != nil || transcript == "" {
		t.Errorf("scripted transcript = %q (%v)", transcript, err)
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
	provider := &ai.OpenAICompat{BaseURL: baseURL, Models: ai.ModelSet{Default: model}}
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

// TestWhisperCLI_Integration runs the real whisper.cpp binary against a
// real model when WHISPER_TEST_MODEL points at a ggml file — the local
// half of ADR-0004's voice path, verified rather than assumed:
//
//	say -o /tmp/earful-speech.wav --data-format=LEI16@16000 "the capital of France is Paris"
//	WHISPER_TEST_MODEL=$HOME/models/ggml-base.bin WHISPER_TEST_AUDIO=/tmp/earful-speech.wav \
//	  go test ./internal/ai/ -run WhisperCLI_Integration -v
func TestWhisperCLI_Integration(t *testing.T) {
	model := os.Getenv("WHISPER_TEST_MODEL")
	audioPath := os.Getenv("WHISPER_TEST_AUDIO")
	if model == "" || audioPath == "" {
		t.Skip("WHISPER_TEST_MODEL / WHISPER_TEST_AUDIO not set; see docs/testing.md")
	}
	audio, err := os.Open(audioPath)
	if err != nil {
		t.Fatalf("open audio: %v", err)
	}
	defer audio.Close()

	bin := os.Getenv("WHISPER_TEST_BIN")
	if bin == "" {
		bin = "whisper-cli"
	}
	provider := &ai.WhisperCLI{Bin: bin, Model: model}
	stream, err := provider.Transcribe(context.Background(), ai.TranscribeRequest{
		Audio: audio, MIMEType: "audio/wav", Language: "en",
	})
	if err != nil {
		t.Fatalf("Transcribe: %v", err)
	}
	transcript, err := ai.Collect(stream)
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}
	if !strings.Contains(strings.ToLower(transcript), "paris") {
		t.Errorf("transcript %q does not contain the spoken word", transcript)
	}
	t.Logf("whisper heard: %q", strings.TrimSpace(transcript))
}
