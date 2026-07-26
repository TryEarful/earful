package ai_test

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/TryEarful/earful/internal/ai"
)

// fakeVertex imitates the Vertex AI streamGenerateContent endpoint,
// recording the path and body so the tests can assert on the wire format
// sent — the part a live call would otherwise be the only witness to.
type vertexCall struct {
	path        string
	contentType string
	body        map[string]any
}

func fakeVertex(t *testing.T, frames ...string) (*httptest.Server, *[]vertexCall) {
	t.Helper()
	var calls []vertexCall
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		calls = append(calls, vertexCall{
			path:        r.URL.Path + "?" + r.URL.RawQuery,
			contentType: r.Header.Get("Content-Type"),
			body:        body,
		})
		w.Header().Set("Content-Type", "text/event-stream")
		for _, frame := range frames {
			fmt.Fprintf(w, "data: %s\n\n", frame)
		}
	}))
	t.Cleanup(srv.Close)
	return srv, &calls
}

func textFrame(text string) string {
	return fmt.Sprintf(`{"candidates":[{"content":{"role":"model","parts":[{"text":%q}]}}]}`, text)
}

func TestVertex_StreamsAndAddressesTheRegionalModel(t *testing.T) {
	t.Parallel()
	srv, calls := fakeVertex(t, textFrame("Themes: "), textFrame("speed, onboarding."))
	provider := &ai.Vertex{
		Project:  "earful-stg",
		Location: "europe-west4",
		Models:   ai.ModelSet{Default: "flash-model", Analyze: "pro-model"},
		Endpoint: srv.URL,
		Client:   srv.Client(),
	}

	stream, err := provider.Analyze(context.Background(), ai.AnalyzeRequest{
		System: "You are an analyst.", Prompt: "80 answers",
	})
	if err != nil {
		t.Fatalf("Analyze: %v", err)
	}
	out, err := ai.Collect(stream)
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}
	if out != "Themes: speed, onboarding." {
		t.Errorf("output = %q", out)
	}

	if len(*calls) != 1 {
		t.Fatalf("calls = %d, want 1", len(*calls))
	}
	call := (*calls)[0]
	// The analyze-tier model is used, not the default: "insights use a
	// stronger tier" is configuration (SPEC "AI surface").
	want := "/v1/projects/earful-stg/locations/europe-west4/publishers/google/models/pro-model:streamGenerateContent?alt=sse"
	if call.path != want {
		t.Errorf("path = %q, want %q", call.path, want)
	}
	if call.body["systemInstruction"] == nil {
		t.Error("system instruction was not sent")
	}
}

func TestVertex_TranscribeSendsAudioInlineWithLanguageHint(t *testing.T) {
	t.Parallel()
	srv, calls := fakeVertex(t, textFrame("hallo daar"))
	provider := &ai.Vertex{
		Project: "earful-stg", Location: "europe-west4",
		Models:   ai.ModelSet{Default: "flash-model"},
		Endpoint: srv.URL, Client: srv.Client(),
	}

	stream, err := provider.Transcribe(context.Background(), ai.TranscribeRequest{
		Audio:    strings.NewReader("RIFFfake-wav-bytes"),
		MIMEType: "audio/wav",
		Language: "Dutch",
	})
	if err != nil {
		t.Fatalf("Transcribe: %v", err)
	}
	text, err := ai.Collect(stream)
	if err != nil || text != "hallo daar" {
		t.Fatalf("transcript = %q (%v)", text, err)
	}

	parts := (*calls)[0].body["contents"].([]any)[0].(map[string]any)["parts"].([]any)
	inline, ok := parts[0].(map[string]any)["inlineData"].(map[string]any)
	if !ok {
		t.Fatalf("audio was not sent inline: %#v", parts[0])
	}
	if inline["mimeType"] != "audio/wav" {
		t.Errorf("mimeType = %v", inline["mimeType"])
	}
	decoded, err := base64.StdEncoding.DecodeString(inline["data"].(string))
	if err != nil || string(decoded) != "RIFFfake-wav-bytes" {
		t.Errorf("inline audio = %q (%v)", decoded, err)
	}
	// The respondent's language must reach the model (M11-T3).
	if prompt, _ := parts[1].(map[string]any)["text"].(string); !strings.Contains(prompt, "Dutch") {
		t.Errorf("language hint missing from prompt: %q", prompt)
	}
}

func TestVertex_SurfacesErrors(t *testing.T) {
	t.Parallel()

	t.Run("http status carries the API message", func(t *testing.T) {
		t.Parallel()
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			http.Error(w, `{"error":{"message":"Publisher Model not found"}}`, http.StatusNotFound)
		}))
		t.Cleanup(srv.Close)
		provider := &ai.Vertex{
			Project: "p", Location: "europe-west4", Models: ai.ModelSet{Default: "nope"},
			Endpoint: srv.URL, Client: srv.Client(),
		}
		_, err := provider.Generate(context.Background(), ai.GenerateRequest{Prompt: "hi"})
		if err == nil || !strings.Contains(err.Error(), "Publisher Model not found") {
			t.Errorf("err = %v, want the API's own message", err)
		}
	})

	t.Run("mid-stream error is not silently truncated", func(t *testing.T) {
		t.Parallel()
		srv, _ := fakeVertex(t, textFrame("partial "), `{"error":{"status":"RESOURCE_EXHAUSTED","message":"quota"}}`)
		provider := &ai.Vertex{
			Project: "p", Location: "europe-west4", Models: ai.ModelSet{Default: "m"},
			Endpoint: srv.URL, Client: srv.Client(),
		}
		stream, err := provider.Generate(context.Background(), ai.GenerateRequest{Prompt: "hi"})
		if err != nil {
			t.Fatalf("Generate: %v", err)
		}
		if _, err := ai.Collect(stream); err == nil || !strings.Contains(err.Error(), "RESOURCE_EXHAUSTED") {
			t.Errorf("err = %v, want the stream error", err)
		}
	})

	t.Run("unconfigured model is an absent feature, not a failure", func(t *testing.T) {
		t.Parallel()
		provider := &ai.Vertex{Project: "p", Location: "europe-west4"}
		if _, err := provider.Generate(context.Background(), ai.GenerateRequest{}); err != ai.ErrUnsupported {
			t.Errorf("err = %v, want ErrUnsupported", err)
		}
	})
}

// TestVertex_Integration runs against real Vertex AI when
// VERTEX_TEST_PROJECT and VERTEX_TEST_MODEL are set, using Application
// Default Credentials — the same credential path Cloud Run uses. It is
// the only way to know the wire format above matches the live API, so it
// is run by hand at least once per change to vertex.go:
//
//	VERTEX_TEST_PROJECT=earful-stg-xxxx VERTEX_TEST_MODEL=<model> \
//	  go test ./internal/ai/ -run Vertex_Integration -v
//
// Set VERTEX_TEST_AUDIO to a 16-bit PCM WAV file to exercise
// transcription too (on macOS: say -o /tmp/hello.wav
// --data-format=LEI16@16000 "the capital of France is Paris").
func TestVertex_Integration(t *testing.T) {
	project := os.Getenv("VERTEX_TEST_PROJECT")
	model := os.Getenv("VERTEX_TEST_MODEL")
	if project == "" || model == "" {
		t.Skip("VERTEX_TEST_PROJECT / VERTEX_TEST_MODEL not set; see docs/testing.md")
	}
	location := os.Getenv("VERTEX_TEST_LOCATION")
	if location == "" {
		location = "europe-west4"
	}
	provider := &ai.Vertex{
		Project:  project,
		Location: location,
		Models:   ai.ModelSet{Default: model},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	stream, err := provider.Generate(ctx, ai.GenerateRequest{
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
	t.Logf("generate: %q", strings.TrimSpace(out))

	audioPath := os.Getenv("VERTEX_TEST_AUDIO")
	if audioPath == "" {
		t.Log("VERTEX_TEST_AUDIO not set; skipping the transcription half")
		return
	}
	audio, err := os.Open(audioPath)
	if err != nil {
		t.Fatalf("open audio: %v", err)
	}
	defer audio.Close()
	stream, err = provider.Transcribe(ctx, ai.TranscribeRequest{
		Audio: audio, MIMEType: "audio/wav", Language: "English",
	})
	if err != nil {
		t.Fatalf("Transcribe: %v", err)
	}
	transcript, err := ai.Collect(stream)
	if err != nil {
		t.Fatalf("Collect transcript: %v", err)
	}
	if strings.TrimSpace(transcript) == "" {
		t.Error("transcription returned nothing")
	}
	t.Logf("transcript: %q", strings.TrimSpace(transcript))
}
