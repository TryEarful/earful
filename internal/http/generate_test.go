package http_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/TryEarful/earful/internal/ai"
	"github.com/TryEarful/earful/internal/apptest"
)

// The model's side of M6-T3: one JSON question per line. Two of these are
// deliberately unusable — a choice question with a single option and an
// invented type — because a model will produce those and a creator must
// not have to notice.
const generatedNDJSON = `{"type":"long_text","text":"What stood out in your first week?","required":true}
{"type":"single_choice","text":"How often do you use it?","options":["Daily","Weekly","Rarely"]}
{"type":"rating_scale","text":"How was onboarding?","scale_min":1,"scale_max":7}
{"type":"single_choice","text":"Broken: one option","options":["Only this"]}
{"type":"mind_reading","text":"What are you thinking?"}
{"type":"nps","text":"How likely are you to recommend us?"}`

func generatorFake() *ai.Fake {
	// Fragment boundaries fall mid-line on purpose: the reader must
	// reassemble lines, not depend on the model's chunking.
	return &ai.Fake{GenerateScript: [][]string{splitEvery(generatedNDJSON, 37)}}
}

func splitEvery(s string, n int) []string {
	var out []string
	for len(s) > n {
		out = append(out, s[:n])
		s = s[n:]
	}
	return append(out, s)
}

// TestGenerate_WithoutJavaScriptAddsEditableQuestions is story 19 and 20
// through the plain form: a prompt in, ordinary draft questions out.
func TestGenerate_WithoutJavaScriptAddsEditableQuestions(t *testing.T) {
	t.Parallel()
	fake := generatorFake()
	app := apptest.New(t, apptest.Options{AI: fake})
	creator := app.Login(t, apptest.UniqueEmail("generate"))
	id := app.CreateSurvey(t, creator, "Generated", true)

	resp := app.PostForm(t, creator, "/surveys/"+id+"/generate", url.Values{
		"prompt": {"the first week of using our product"},
	})
	defer resp.Body.Close()
	body := apptest.ReadBody(t, resp)

	// Four of the six lines are valid questions; the other two are
	// reported, not silently dropped.
	if !bodyContains(body, "Added 4 questions") || !bodyContains(body, "2 were skipped") {
		t.Errorf("generation notice was not honest about what happened:\n%s", body)
	}
	for _, want := range []string{
		"What stood out in your first week?",
		"How often do you use it?",
		"How was onboarding?",
		"How likely are you to recommend us?",
	} {
		if !bodyContains(body, want) {
			t.Errorf("generated question %q is not in the draft:\n%s", want, body)
		}
	}
	if bodyContains(body, "What are you thinking?") {
		t.Error("a question of an invented type reached the draft")
	}

	// The prompt reached the model with the question-format instructions.
	if len(fake.GenerateCalls) != 1 {
		t.Fatalf("model calls = %d, want 1", len(fake.GenerateCalls))
	}
	call := fake.GenerateCalls[0]
	if !strings.Contains(call.Prompt, "first week") {
		t.Errorf("prompt = %q", call.Prompt)
	}
	if !strings.Contains(call.System, "long_text") || !strings.Contains(call.System, "one JSON object per line") {
		t.Errorf("system prompt does not constrain the output shape:\n%s", call.System)
	}

	// They are ordinary draft content: editable, and one save, so the
	// audit log has a single entry for the run.
	identities := app.QuestionIdentities(t, creator, id)
	if len(identities) != 4 {
		t.Fatalf("draft holds %d questions, want 4", len(identities))
	}
	edit := app.PostForm(t, creator, "/surveys/"+id+"/questions/"+identities[0], url.Values{
		"type": {"long_text"}, "text": {"Reworded by hand"},
	})
	edit.Body.Close()
	if page := app.SurveyPage(t, creator, id); !bodyContains(page, "Reworded by hand") {
		t.Error("a generated question could not be edited like any other")
	}

	// And the published version carries the model's rating scale, not a
	// default: 1..7 as generated.
	app.Publish(t, creator, id)
	respondent := &http.Client{}
	page := mustGet(t, respondent, app.Server.URL+"/s/"+id)
	if !strings.Contains(page, `value="7"`) {
		t.Errorf("the generated 1..7 scale did not survive publishing:\n%s", page)
	}
}

// TestGenerate_StreamsOverTheSocket is story 19's "watch them stream in":
// the same operation, with the output visible as it arrives, and exactly
// one model call for it.
func TestGenerate_StreamsOverTheSocket(t *testing.T) {
	t.Parallel()
	fake := generatorFake()
	app := apptest.New(t, apptest.Options{AI: fake})
	creator := app.Login(t, apptest.UniqueEmail("generatews"))
	id := app.CreateSurvey(t, creator, "Streamed", true)

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	conn := dialAuthed(t, ctx, app, creator, "/surveys/"+id+"/generate/stream")
	defer conn.CloseNow()

	start, _ := json.Marshal(map[string]any{
		"action": "generate",
		"params": map[string]string{"prompt": "onboarding"},
	})
	if err := conn.Write(ctx, websocket.MessageText, start); err != nil {
		t.Fatalf("send generate: %v", err)
	}

	var streamed strings.Builder
	var chunks int
	for {
		_, data, err := conn.Read(ctx)
		if err != nil {
			t.Fatalf("read frame: %v", err)
		}
		var frame voiceFrame // same envelope for every stream
		if err := json.Unmarshal(data, &frame); err != nil {
			t.Fatalf("decode frame: %v", err)
		}
		if frame.Type == "chunk" {
			chunks++
			streamed.WriteString(frame.Text)
			continue
		}
		if frame.Type == "error" {
			t.Fatalf("generation failed: %+v", frame)
		}
		if frame.Type == "done" {
			break
		}
	}
	if chunks < 2 {
		t.Errorf("output arrived in %d frames; it is supposed to stream", chunks)
	}
	if !strings.Contains(streamed.String(), "What stood out in your first week?") {
		t.Errorf("streamed output missing the questions:\n%s", streamed.String())
	}
	if len(fake.GenerateCalls) != 1 {
		t.Errorf("model calls = %d, want exactly 1 — streaming must not cost double", len(fake.GenerateCalls))
	}

	// And the questions are in the draft, ready to edit.
	if page := app.SurveyPage(t, creator, id); !bodyContains(page, "What stood out in your first week?") {
		t.Errorf("streamed questions never reached the draft:\n%s", page)
	}
}

// TestGenerate_QuotaIsRefusedKindly: exhausting the allowance must read as
// a temporary state, and must not touch the model (stories 21, 67).
func TestGenerate_QuotaIsRefusedKindly(t *testing.T) {
	t.Parallel()
	fake := generatorFake()
	app := apptest.New(t, apptest.Options{AI: fake, AIQuota: 1})
	creator := app.Login(t, apptest.UniqueEmail("generatequota"))
	id := app.CreateSurvey(t, creator, "Quota", true)

	// The first run spends the allowance.
	app.PostForm(t, creator, "/surveys/"+id+"/generate", url.Values{"prompt": {"first"}}).Body.Close()

	resp := app.PostForm(t, creator, "/surveys/"+id+"/generate", url.Values{"prompt": {"second"}})
	defer resp.Body.Close()
	body := apptest.ReadBody(t, resp)
	if !bodyContains(body, "used its AI allowance for today") {
		t.Errorf("quota refusal is not readable:\n%s", body)
	}
	if len(fake.GenerateCalls) != 1 {
		t.Errorf("model calls = %d; a refused request must not reach the provider", len(fake.GenerateCalls))
	}
	// The rest of the editor keeps working.
	if !bodyContains(body, "Add a question") {
		t.Error("a quota refusal broke the editor page")
	}
}

// TestGenerate_IsAbsentWithoutAProvider: no text AI configured, no panel,
// and the endpoint says so rather than half-working (Appendix D).
func TestGenerate_IsAbsentWithoutAProvider(t *testing.T) {
	t.Parallel()
	app := apptest.New(t, apptest.Options{})
	creator := app.Login(t, apptest.UniqueEmail("noai"))
	id := app.CreateSurvey(t, creator, "No AI", true)

	page := app.SurveyPage(t, creator, id)
	if bodyContains(page, "Draft questions with AI") {
		t.Errorf("the AI panel was offered with no provider configured:\n%s", page)
	}

	resp := app.PostForm(t, creator, "/surveys/"+id+"/generate", url.Values{"prompt": {"anything"}})
	defer resp.Body.Close()
	body := apptest.ReadBody(t, resp)
	if !bodyContains(body, "AI isn't configured") {
		t.Errorf("expected an honest explanation, got:\n%s", body)
	}
}

// TestGenerate_CrossWorkspaceDenied: another workspace's survey is not
// generable into, and is indistinguishable from one that doesn't exist.
func TestGenerate_CrossWorkspaceDenied(t *testing.T) {
	t.Parallel()
	app := apptest.New(t, apptest.Options{AI: generatorFake()})
	owner := app.Login(t, apptest.UniqueEmail("owner"))
	stranger := app.Login(t, apptest.UniqueEmail("stranger"))
	id := app.CreateSurvey(t, owner, "Private", true)

	resp := app.PostForm(t, stranger, "/surveys/"+id+"/generate", url.Values{"prompt": {"peek"}})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("status = %d, want 404", resp.StatusCode)
	}
}

// dialAuthed opens a WebSocket carrying the creator's session cookies,
// the way the browser does from a page they are signed in to.
func dialAuthed(t *testing.T, ctx context.Context, app *apptest.App, client *http.Client, path string) *websocket.Conn {
	t.Helper()
	header := http.Header{"Origin": {app.Server.URL}}
	base := mustParseURL(t, app.Server.URL)
	var cookies []string
	for _, c := range client.Jar.Cookies(base) {
		cookies = append(cookies, c.Name+"="+c.Value)
	}
	if len(cookies) > 0 {
		header.Set("Cookie", strings.Join(cookies, "; "))
	}
	conn, resp, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(app.Server.URL, "http")+path,
		&websocket.DialOptions{HTTPHeader: header})
	if err != nil {
		status := 0
		if resp != nil {
			status = resp.StatusCode
		}
		t.Fatalf("dial %s: %v (status %d)", path, err, status)
	}
	return conn
}
