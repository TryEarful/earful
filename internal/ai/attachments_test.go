package ai_test

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/TryEarful/earful/internal/ai"
	"github.com/TryEarful/earful/internal/clock"
)

func TestVertex_SendsAttachmentsInlineBeforeThePrompt(t *testing.T) {
	t.Parallel()
	srv, calls := fakeVertex(t, textFrame("ok"))
	provider := &ai.Vertex{
		Project: "p", Location: "eu", Models: ai.ModelSet{Default: "m"},
		Endpoint: srv.URL, Client: srv.Client(),
	}
	pdf := []byte("%PDF-1.4 tiny")
	stream, err := provider.Generate(context.Background(), ai.GenerateRequest{
		Prompt: "use the attached",
		Attachments: []ai.Attachment{
			{Name: "brief.pdf", MIME: ai.MIMEPDF, Data: pdf},
			{Name: "notes.md", MIME: ai.MIMEText, Data: []byte("# Notes")},
		},
	})
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if _, err := ai.Collect(stream); err != nil {
		t.Fatalf("Collect: %v", err)
	}

	contents := (*calls)[0].body["contents"].([]any)
	parts := contents[0].(map[string]any)["parts"].([]any)
	if len(parts) != 5 {
		t.Fatalf("parts = %d, want a name and a blob per file and the prompt: %v", len(parts), parts)
	}
	if got := parts[0].(map[string]any)["text"]; got != "Attached file: brief.pdf" {
		t.Errorf("first part = %v", got)
	}
	blob := parts[1].(map[string]any)["inlineData"].(map[string]any)
	if blob["mimeType"] != ai.MIMEPDF || blob["data"] != base64.StdEncoding.EncodeToString(pdf) {
		t.Errorf("pdf blob = %v", blob)
	}
	if blob := parts[3].(map[string]any)["inlineData"].(map[string]any); blob["mimeType"] != ai.MIMEText {
		t.Errorf("text blob = %v", blob)
	}
	if got := parts[4].(map[string]any)["text"]; got != "use the attached" {
		t.Errorf("last part = %v, want the prompt", got)
	}
}

func TestVertex_RefusesAKindItCannotRead(t *testing.T) {
	t.Parallel()
	provider := &ai.Vertex{Project: "p", Location: "eu", Models: ai.ModelSet{Default: "m"}, Endpoint: "http://127.0.0.1:1"}
	_, err := provider.Generate(context.Background(), ai.GenerateRequest{
		Prompt:      "p",
		Attachments: []ai.Attachment{{Name: "x.svg", MIME: "image/svg+xml", Data: []byte("<svg/>")}},
	})
	if !errors.Is(err, ai.ErrAttachmentUnsupported) {
		t.Errorf("err = %v, want ErrAttachmentUnsupported", err)
	}
}

func TestOpenAICompat_TextAttachmentsJoinThePrompt(t *testing.T) {
	t.Parallel()
	var sent string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Messages []struct {
				Role    string `json:"role"`
				Content string `json:"content"`
			} `json:"messages"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		sent = body.Messages[len(body.Messages)-1].Content
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	t.Cleanup(srv.Close)
	provider := &ai.OpenAICompat{BaseURL: srv.URL, Models: ai.ModelSet{Default: "m"}}

	stream, err := provider.Generate(context.Background(), ai.GenerateRequest{
		Prompt: "use these",
		Attachments: []ai.Attachment{
			{Name: "a.csv", MIME: ai.MIMEText, Data: []byte("q,a\nWhy,Because\n")},
			{Name: "b.md", MIME: ai.MIMEText, Data: []byte("```go\nx\n```")},
		},
	})
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	_, _ = ai.Collect(stream)

	want := "use these\n\nAttached file: a.csv\n```\nq,a\nWhy,Because\n```" +
		"\n\nAttached file: b.md\n````\n```go\nx\n```\n````"
	if sent != want {
		t.Errorf("prompt =\n%s\nwant\n%s", sent, want)
	}
}

func TestOpenAICompat_RefusesImagesAndPDF(t *testing.T) {
	t.Parallel()
	provider := &ai.OpenAICompat{BaseURL: "http://127.0.0.1:1", Models: ai.ModelSet{Default: "m"}}
	for _, mime := range []string{ai.MIMEPNG, ai.MIMEJPEG, ai.MIMEPDF} {
		_, err := provider.Generate(context.Background(), ai.GenerateRequest{
			Prompt:      "p",
			Attachments: []ai.Attachment{{Name: "f", MIME: mime, Data: []byte("x")}},
		})
		if !errors.Is(err, ai.ErrAttachmentUnsupported) {
			t.Errorf("%s: err = %v, want ErrAttachmentUnsupported", mime, err)
		}
	}
}

func TestScriptedAndFake_AcknowledgeAttachments(t *testing.T) {
	t.Parallel()
	req := ai.GenerateRequest{
		Prompt:      "topic",
		Attachments: []ai.Attachment{{Name: "notes.md", MIME: ai.MIMEText}, {Name: "data.csv", MIME: ai.MIMEText}},
	}
	stream, err := (&ai.Scripted{}).Generate(context.Background(), req)
	if err != nil {
		t.Fatalf("Scripted: %v", err)
	}
	if out, _ := ai.Collect(stream); !strings.Contains(out, "notes.md, data.csv") {
		t.Errorf("scripted output does not name the files: %s", out)
	}

	// Through a composite, as the server holds it.
	fake := &ai.Fake{GenerateScript: [][]string{{"generated"}}}
	stream, err = (&ai.Composite{Text: fake}).Generate(context.Background(), req)
	if err != nil {
		t.Fatalf("Fake: %v", err)
	}
	if out, _ := ai.Collect(stream); !strings.Contains(out, "attached: notes.md, data.csv") {
		t.Errorf("fake output does not name the files: %q", out)
	}
	if len(fake.GenerateCalls[0].Attachments) != 2 {
		t.Errorf("composite dropped attachments: %v", fake.GenerateCalls[0].Attachments)
	}
}

func TestMeter_CheckForCountsTheEstimate(t *testing.T) {
	t.Parallel()
	store := &memoryUsage{}
	meter := newMeter(store, clock.NewFake(time.Now()))
	ctx := context.Background()
	ws := uuid.New()

	// The normal cap is 1000 tokens: an estimate past it is refused on a
	// fresh day, one under it is not, and once recorded it counts.
	if err := meter.CheckFor(ctx, ws, 1001); !errors.Is(err, ai.ErrQuotaExceeded) {
		t.Errorf("over-cap estimate: err = %v, want ErrQuotaExceeded", err)
	}
	if err := meter.CheckFor(ctx, ws, 600); err != nil {
		t.Fatalf("under-cap estimate refused: %v", err)
	}
	if err := meter.RecordWith(ctx, ws, nil, "generate", 0, 600); err != nil {
		t.Fatalf("record: %v", err)
	}
	if err := meter.CheckFor(ctx, ws, 600); !errors.Is(err, ai.ErrQuotaExceeded) {
		t.Errorf("second estimate: err = %v, want ErrQuotaExceeded", err)
	}
	if err := meter.Check(ctx, ws); err != nil {
		t.Errorf("plain check under the cap refused: %v", err)
	}
}

func TestEstimateTokens_ErrsHighForBinaryKinds(t *testing.T) {
	t.Parallel()
	text := ai.EstimateTokens([]ai.Attachment{{MIME: ai.MIMEText, Data: make([]byte, 4000)}})
	image := ai.EstimateTokens([]ai.Attachment{{MIME: ai.MIMEPNG, Data: make([]byte, 10)}})
	pdf := ai.EstimateTokens([]ai.Attachment{{MIME: ai.MIMEPDF, Data: make([]byte, 16000)}})
	if text != 1001 {
		t.Errorf("text = %d, want chars/4+1", text)
	}
	if image < 258 {
		t.Errorf("image = %d, want at least one tile", image)
	}
	if pdf < 1258 {
		t.Errorf("pdf = %d, want at least a page and its bytes", pdf)
	}
}
