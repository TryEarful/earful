package http_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/TryEarful/earful/internal/ai"
	"github.com/TryEarful/earful/internal/apptest"
)

// Files attached to an AI prompt (issue #5): the drafting panel and the
// new-survey form take them as multipart posts, check them, and send
// them to the model with the prompt.

func file(name, content string) apptest.Upload {
	return apptest.Upload{Field: "files", Name: name, Data: []byte(content)}
}

func generateWith(t *testing.T, app *apptest.App, client *http.Client, id, prompt string, files ...apptest.Upload) (*http.Response, string) {
	t.Helper()
	resp := app.PostMultipart(t, client, "/surveys/"+id+"/generate", url.Values{"prompt": {prompt}}, files...)
	defer resp.Body.Close()
	return resp, apptest.ReadBody(t, resp)
}

func TestAttach_FilesReachTheModelWithThePrompt(t *testing.T) {
	t.Parallel()
	fake := generatorFake()
	app := apptest.New(t, apptest.Options{AI: fake})
	creator := app.Login(t, apptest.UniqueEmail("attach"))
	id := app.CreateSurvey(t, creator, "Attached", true)

	if page := app.SurveyPage(t, creator, id); !strings.Contains(page, `enctype="multipart/form-data"`) ||
		!strings.Contains(page, `name="files"`) || !bodyContains(page, "Attach files") {
		t.Fatalf("the drafting panel does not take files:\n%s", page)
	}

	resp, body := generateWith(t, app, creator, id, "use the attached questions",
		file("notes.md", "# What to ask\n\n- first week"),
		file("answers.csv", "question,answer\nHow did you find us?,A friend\n"))
	if resp.StatusCode != http.StatusOK || !bodyContains(body, "Added 4 questions") {
		t.Fatalf("status %d; the run did not draft questions:\n%s", resp.StatusCode, body)
	}

	if len(fake.GenerateCalls) != 1 {
		t.Fatalf("model calls = %d, want 1", len(fake.GenerateCalls))
	}
	call := fake.GenerateCalls[0]
	if call.Prompt != "use the attached questions" {
		t.Errorf("prompt = %q", call.Prompt)
	}
	if len(call.Attachments) != 2 {
		t.Fatalf("attachments = %+v, want both files", call.Attachments)
	}
	if a := call.Attachments[0]; a.Name != "notes.md" || a.MIME != ai.MIMEText || !strings.Contains(string(a.Data), "first week") {
		t.Errorf("first attachment = %s %s %q", a.Name, a.MIME, a.Data)
	}
	if a := call.Attachments[1]; a.Name != "answers.csv" || !strings.Contains(string(a.Data), "A friend") {
		t.Errorf("second attachment = %s %q", a.Name, a.Data)
	}
}

func TestAttach_AFileWithoutAPromptIsEnough(t *testing.T) {
	t.Parallel()
	fake := generatorFake()
	app := apptest.New(t, apptest.Options{AI: fake})
	creator := app.Login(t, apptest.UniqueEmail("attach-alone"))
	id := app.CreateSurvey(t, creator, "File alone", true)

	_, body := generateWith(t, app, creator, id, "", file("brief.txt", "Ask new staff about their first month."))
	if !bodyContains(body, "Added 4 questions") {
		t.Fatalf("a file alone did not draft questions:\n%s", body)
	}
	if call := fake.GenerateCalls[0]; !strings.Contains(call.Prompt, "attached files") || len(call.Attachments) != 1 {
		t.Errorf("the model was not asked about the file: %q %+v", call.Prompt, call.Attachments)
	}

	// With neither, the panel still asks for a description.
	resp, body := generateWith(t, app, creator, id, "")
	if resp.StatusCode != http.StatusUnprocessableEntity || !bodyContains(body, "Describe what you want to ask about") {
		t.Errorf("status %d for an empty form:\n%s", resp.StatusCode, body)
	}
}

func TestAttach_RefusesAFileAndKeepsTheForm(t *testing.T) {
	t.Parallel()
	cases := []struct {
		label string
		file  apptest.Upload
		want  string
	}{
		{"a type off the list", file("run.exe", "MZ\x90\x00"), "run.exe can't be used"},
		{"an image named as text", file("notes.txt", "\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDR"), "notes.txt can't be used"},
		{"a legacy format", file("old.doc", "\xd0\xcf\x11\xe0\xa1\xb1\x1a\xe1"), "Save it as .docx"},
		{"one file over the cap", apptest.Upload{Field: "files", Name: "big.txt", Data: bytes.Repeat([]byte("a"), 5<<20+1)}, "big.txt is over 5 MB"},
	}
	fake := generatorFake()
	app := apptest.New(t, apptest.Options{AI: fake})
	creator := app.Login(t, apptest.UniqueEmail("attach-refused"))
	id := app.CreateSurvey(t, creator, "Refused", true)

	for _, c := range cases {
		resp, body := generateWith(t, app, creator, id, "keep this prompt", c.file)
		if resp.StatusCode != http.StatusUnprocessableEntity {
			t.Errorf("%s: status %d, want 422", c.label, resp.StatusCode)
		}
		if !bodyContains(body, c.want) {
			t.Errorf("%s: the refusal does not say %q:\n%s", c.label, c.want, body)
		}
		if !bodyContains(body, ">keep this prompt</textarea>") {
			t.Errorf("%s: the prompt was not handed back", c.label)
		}
	}
	if len(fake.GenerateCalls) != 0 {
		t.Errorf("a refused file still reached the model %d times", len(fake.GenerateCalls))
	}
}

// TestAttach_RefusesABodyOverTheRequestCap: past the cap the body is cut
// off, so there is no form to hand back, only a page that says why.
// Other routes keep the tighter cap.
func TestAttach_RefusesABodyOverTheRequestCap(t *testing.T) {
	t.Parallel()
	fake := generatorFake()
	app := apptest.New(t, apptest.Options{AI: fake})
	creator := app.Login(t, apptest.UniqueEmail("attach-huge"))
	id := app.CreateSurvey(t, creator, "Huge", true)

	huge := apptest.Upload{Field: "files", Name: "huge.txt", Data: bytes.Repeat([]byte("a"), 13<<20)}
	resp, body := generateWith(t, app, creator, id, "too much", huge)
	if resp.StatusCode != http.StatusRequestEntityTooLarge || !bodyContains(body, "Files too large") {
		t.Errorf("status %d for a 13 MB body:\n%.500s", resp.StatusCode, body)
	}
	if len(fake.GenerateCalls) != 0 {
		t.Error("an oversized body reached the model")
	}

	// Within the upload cap but past every other route's.
	resp = app.PostMultipart(t, creator, "/surveys/"+id+"/participants", url.Values{},
		apptest.Upload{Field: "csv", Name: "list.csv", Data: bytes.Repeat([]byte("a"), 5<<20)})
	resp.Body.Close()
	if resp.StatusCode == http.StatusOK || resp.StatusCode == http.StatusSeeOther {
		t.Errorf("a 5 MB body was accepted by a route capped at 4 MB: status %d", resp.StatusCode)
	}
}

// TestAttach_CountsTowardTheAllowance: the files' estimate is checked
// before the call and charged with it, so an upload cannot overrun a
// workspace's daily cap.
func TestAttach_CountsTowardTheAllowance(t *testing.T) {
	t.Parallel()
	fake := generatorFake()
	app := apptest.New(t, apptest.Options{AI: fake, AIQuota: 3000})
	creator := app.Login(t, apptest.UniqueEmail("attach-meter"))
	id := app.CreateSurvey(t, creator, "Metered", true)

	// About 2000 tokens of text: under the cap once, not twice.
	notes := file("notes.txt", strings.Repeat("word ", 1600))
	if _, body := generateWith(t, app, creator, id, "first", notes); !bodyContains(body, "Added 4 questions") {
		t.Fatalf("the first upload was refused:\n%s", body)
	}
	resp, body := generateWith(t, app, creator, id, "second", notes)
	if resp.StatusCode != http.StatusUnprocessableEntity || !bodyContains(body, "used today's AI allowance") {
		t.Errorf("status %d; the second upload was not refused for the allowance:\n%s", resp.StatusCode, body)
	}
	if len(fake.GenerateCalls) != 1 {
		t.Errorf("model calls = %d, want only the first", len(fake.GenerateCalls))
	}

	// One file larger than a whole day's allowance is refused outright.
	fresh := apptest.New(t, apptest.Options{AI: generatorFake(), AIQuota: 1000})
	other := fresh.Login(t, apptest.UniqueEmail("attach-meter-big"))
	otherID := fresh.CreateSurvey(t, other, "Too big for the day", true)
	if _, body := generateWith(t, fresh, other, otherID, "p", notes); !bodyContains(body, "used today's AI allowance") {
		t.Errorf("an upload past the whole cap was not refused:\n%s", body)
	}
}

func TestAttach_AProviderThatCannotReadTheFileSaysSo(t *testing.T) {
	t.Parallel()
	fake := &ai.Fake{Err: fmt.Errorf("%w: photo.png (image/png)", ai.ErrAttachmentUnsupported)}
	app := apptest.New(t, apptest.Options{AI: fake})
	creator := app.Login(t, apptest.UniqueEmail("attach-textonly"))
	id := app.CreateSurvey(t, creator, "Text only", true)

	resp, body := generateWith(t, app, creator, id, "about this", file("photo.png", "\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDR"))
	if resp.StatusCode != http.StatusUnprocessableEntity || !bodyContains(body, "reads text only") {
		t.Errorf("status %d:\n%s", resp.StatusCode, body)
	}
}

type flagEverything struct{ sums int }

func (f *flagEverything) Malicious(context.Context, [sha256.Size]byte) (bool, error) {
	f.sums++
	return true, nil
}

func TestAttach_AFlaggedFileIsRefused(t *testing.T) {
	t.Parallel()
	fake := generatorFake()
	scanner := &flagEverything{}
	app := apptest.New(t, apptest.Options{AI: fake, AttachScanner: scanner})
	creator := app.Login(t, apptest.UniqueEmail("attach-flagged"))
	id := app.CreateSurvey(t, creator, "Flagged", true)

	resp, body := generateWith(t, app, creator, id, "p", file("notes.md", "# Notes"))
	if resp.StatusCode != http.StatusUnprocessableEntity || !bodyContains(body, "notes.md was reported as harmful") {
		t.Errorf("status %d:\n%s", resp.StatusCode, body)
	}
	if scanner.sums != 1 || len(fake.GenerateCalls) != 0 {
		t.Errorf("lookups = %d, model calls = %d", scanner.sums, len(fake.GenerateCalls))
	}
}

func TestAttach_ASurveyStartsFromAFile(t *testing.T) {
	t.Parallel()
	fake := titledFake()
	app := apptest.New(t, apptest.Options{AI: fake})
	creator := app.Login(t, apptest.UniqueEmail("attach-new"))

	if page := mustGet(t, creator, app.Server.URL+"/surveys/new"); !strings.Contains(page, `enctype="multipart/form-data"`) ||
		!strings.Contains(page, `name="files"`) {
		t.Fatalf("the new-survey form does not take files:\n%s", page)
	}

	// A wrong file hands the form back as typed and creates nothing.
	resp := app.PostMultipart(t, creator, "/surveys", url.Values{
		"title": {""}, "prompt": {"onboarding"}, "anonymity": {"anonymous"},
	}, file("run.exe", "MZ"))
	body := apptest.ReadBody(t, resp)
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnprocessableEntity || !bodyContains(body, "run.exe can't be used") ||
		!bodyContains(body, ">onboarding</textarea>") {
		t.Errorf("status %d; the refused form was not handed back:\n%s", resp.StatusCode, body)
	}
	if len(fake.GenerateCalls) != 0 {
		t.Fatal("a refused file reached the model")
	}

	// A file and no description is enough.
	resp = app.PostMultipart(t, creator, "/surveys", url.Values{
		"title": {""}, "prompt": {""}, "anonymity": {"anonymous"},
	}, file("plan.md", "# Ask about the first week"))
	body = apptest.ReadBody(t, resp)
	resp.Body.Close()
	if !strings.HasPrefix(resp.Request.URL.Path, "/surveys/") || !bodyContains(body, "First week with us") {
		t.Fatalf("landed on %s; the survey was not drafted from the file:\n%s", resp.Request.URL, body)
	}
	if call := fake.GenerateCalls[0]; len(call.Attachments) != 1 || call.Attachments[0].Name != "plan.md" {
		t.Errorf("attachments = %+v", call.Attachments)
	}
}

func TestTrust_ListsVirusTotalOnlyWhenConfigured(t *testing.T) {
	t.Parallel()
	without := apptest.New(t, apptest.Options{})
	if page := mustGet(t, &http.Client{}, without.Server.URL+"/trust"); strings.Contains(page, "VirusTotal") {
		t.Error("an instance without a VirusTotal key lists VirusTotal")
	}
	with := apptest.New(t, apptest.Options{VirusTotalAPIKey: "vt-key"})
	page := mustGet(t, &http.Client{}, with.Server.URL+"/trust")
	if !strings.Contains(page, "VirusTotal") || !bodyContains(page, "never the file") {
		t.Errorf("a configured VirusTotal is not disclosed:\n%s", page)
	}
	if bodyContains(page, "runs entirely on its operator's own infrastructure") {
		t.Error("an instance that calls VirusTotal says no outside company is involved")
	}
}
