package http_test

import (
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"testing"

	"github.com/TryEarful/earful/internal/apptest"
	"github.com/TryEarful/earful/internal/export"
)

// saveStyle posts the Style tab's form and returns the page the creator
// lands on.
func saveStyle(t *testing.T, app *apptest.App, creator *http.Client, id, theme string) (*http.Response, string) {
	t.Helper()
	resp := app.PostForm(t, creator, "/surveys/"+id+"/style", url.Values{"theme": {theme}})
	defer resp.Body.Close()
	return resp, apptest.ReadBody(t, resp)
}

var htmlTagRe = regexp.MustCompile(`<html[^>]*>`)

// themeOf is the theme a page is drawn in: the class on its <html>, or
// empty for Earful's own look, which has none.
func themeOf(t *testing.T, page string) string {
	t.Helper()
	tag := htmlTagRe.FindString(page)
	if tag == "" {
		t.Fatalf("the page has no <html>:\n%s", page)
	}
	m := regexp.MustCompile(`class="theme-([a-z]+)"`).FindStringSubmatch(tag)
	if m == nil {
		if strings.Contains(tag, "class=") {
			t.Fatalf("<html> has a class that is not a theme: %s", tag)
		}
		return ""
	}
	return m[1]
}

// TestStyle_ThemeIsPreviewedThenPublished: a chosen theme is the draft's,
// so the creator's preview has it at once and respondents only when a
// version is published with it; from then on every page of the survey a
// respondent can land on is drawn in it.
func TestStyle_ThemeIsPreviewedThenPublished(t *testing.T) {
	t.Parallel()
	app := apptest.New(t, apptest.Options{})
	creator := app.Login(t, apptest.UniqueEmail("style-publish"))
	id := app.CreateSurvey(t, creator, "Styled", true)
	app.AddQuestion(t, creator, id, "short_text", "Anything?", nil)
	app.Publish(t, creator, id)

	tab := mustGet(t, creator, app.Server.URL+"/surveys/"+id+"/style")
	if !regexp.MustCompile(`name="theme" value="earful" checked`).MatchString(tab) {
		t.Fatalf("a survey with no style does not show the default theme chosen:\n%s", tab)
	}

	resp, saved := saveStyle(t, app, creator, id, "ocean")
	if resp.StatusCode != http.StatusOK || !bodyContains(saved, "Style saved") {
		t.Fatalf("saving a theme: status %d\n%s", resp.StatusCode, saved)
	}
	if !regexp.MustCompile(`name="theme" value="ocean" checked`).MatchString(saved) {
		t.Errorf("the Style tab does not show the saved theme chosen:\n%s", saved)
	}
	// The creator's own pages are never drawn in a survey's theme.
	if got := themeOf(t, saved); got != "" {
		t.Errorf("the Style tab is drawn in theme %q", got)
	}

	if got := themeOf(t, mustGet(t, creator, app.Server.URL+"/surveys/"+id+"/preview")); got != "ocean" {
		t.Errorf("preview theme = %q, want ocean", got)
	}
	previewed := app.PostForm(t, creator, "/surveys/"+id+"/preview", nil)
	if got := themeOf(t, apptest.ReadBody(t, previewed)); got != "ocean" {
		t.Errorf("the submitted preview's theme = %q, want ocean", got)
	}
	previewed.Body.Close()

	live := mustGet(t, jarClient(t), app.Server.URL+"/s/"+id)
	if got := themeOf(t, live); got != "" {
		t.Fatalf("the draft's theme reached respondents before it was published: %q", got)
	}

	if editor := app.SurveyPage(t, creator, id); !bodyContains(editor, "Publish version 2") {
		t.Fatalf("a changed style does not offer a publish:\n%s", editor)
	}
	if body := app.Publish(t, creator, id); !bodyContains(body, "Published version 2") {
		t.Fatalf("publishing the style was refused:\n%s", body)
	}

	live = mustGet(t, jarClient(t), app.Server.URL+"/s/"+id)
	if got := themeOf(t, live); got != "ocean" {
		t.Errorf("the published theme = %q, want ocean", got)
	}
	// The browser's own chrome takes the theme's ground, not Earful's.
	if !strings.Contains(live, `content="#EEF4FB" media="(prefers-color-scheme: light)"`) ||
		!strings.Contains(live, `content="#081A36" media="(prefers-color-scheme: dark)"`) {
		t.Errorf("the theme-color tags do not carry the theme's grounds:\n%s", live)
	}
	if got := themeOf(t, answerOnce(t, app, "/s/"+id, "hello")); got != "ocean" {
		t.Errorf("thanks page theme = %q, want ocean", got)
	}

	// Closed, the survey still looks like itself.
	app.PostForm(t, creator, "/surveys/"+id+"/close", nil).Body.Close()
	closed, err := jarClient(t).Get(app.Server.URL + "/s/" + id)
	if err != nil {
		t.Fatal(err)
	}
	defer closed.Body.Close()
	if closed.StatusCode != http.StatusGone {
		t.Fatalf("closed survey: status %d, want 410", closed.StatusCode)
	}
	if got := themeOf(t, apptest.ReadBody(t, closed)); got != "ocean" {
		t.Errorf("closed page theme = %q, want ocean", got)
	}

	// A link that leads to no survey has no survey to look like.
	missing := mustGet(t, jarClient(t), app.Server.URL+"/s/00000000-0000-0000-0000-000000000000")
	if got := themeOf(t, missing); got != "" {
		t.Errorf("a missing survey's page is drawn in theme %q", got)
	}
}

// TestStyle_FrozenPerVersion is ADR-0001 for the style: a respondent who
// was served version 1 is thanked in version 1's theme, whatever was
// published while they were answering; and choosing the default again is
// a change worth publishing.
func TestStyle_FrozenPerVersion(t *testing.T) {
	t.Parallel()
	app := apptest.New(t, apptest.Options{})
	creator := app.Login(t, apptest.UniqueEmail("style-frozen"))
	id := app.CreateSurvey(t, creator, "Frozen style", true)
	app.AddQuestion(t, creator, id, "short_text", "Anything?", nil)
	saveStyle(t, app, creator, id, "forest")
	app.Publish(t, creator, id)

	// A respondent opens version 1 and is still answering it.
	respondent := jarClient(t)
	page := mustGet(t, respondent, app.Server.URL+"/s/"+id)
	if got := themeOf(t, page); got != "forest" {
		t.Fatalf("version 1's theme = %q, want forest", got)
	}
	form := respondForm(t, page)
	form.Set("q_"+extractAnswerFields(t, page)[0], "from version 1")

	saveStyle(t, app, creator, id, "slate")
	if body := app.Publish(t, creator, id); !bodyContains(body, "Published version 2") {
		t.Fatalf("publishing a second theme was refused:\n%s", body)
	}

	resp, thanks := submitAfterReading(t, app, respondent, id, form)
	if resp.StatusCode != http.StatusOK || !bodyContains(thanks, "Thank you") {
		t.Fatalf("submit: status %d\n%s", resp.StatusCode, thanks)
	}
	if got := themeOf(t, thanks); got != "forest" {
		t.Errorf("a response to version 1 was thanked in theme %q, want forest", got)
	}
	if got := themeOf(t, mustGet(t, jarClient(t), app.Server.URL+"/s/"+id)); got != "slate" {
		t.Errorf("version 2's theme = %q, want slate", got)
	}

	// Back to the default is a change too.
	saveStyle(t, app, creator, id, "earful")
	if body := app.Publish(t, creator, id); !bodyContains(body, "Published version 3") {
		t.Fatalf("publishing the default theme was refused:\n%s", body)
	}
	if got := themeOf(t, mustGet(t, jarClient(t), app.Server.URL+"/s/"+id)); got != "" {
		t.Errorf("version 3's theme = %q, want Earful's own", got)
	}
	// And saving what is already live is not.
	saveStyle(t, app, creator, id, "earful")
	if editor := app.SurveyPage(t, creator, id); bodyContains(editor, "Publish version 4") {
		t.Errorf("an unchanged style offers a publish:\n%s", editor)
	}
}

// TestStyle_RefusesAnUnknownTheme: the theme becomes a class on a
// respondent's page, so only one the stylesheet has is stored. A refused
// theme is answered with a message and leaves the draft as it was.
func TestStyle_RefusesAnUnknownTheme(t *testing.T) {
	t.Parallel()
	app := apptest.New(t, apptest.Options{})
	creator := app.Login(t, apptest.UniqueEmail("style-refuse"))
	id := app.CreateSurvey(t, creator, "Refused style", true)
	app.AddQuestion(t, creator, id, "short_text", "Anything?", nil)
	saveStyle(t, app, creator, id, "slate")

	for _, theme := range []string{"midnight", `ocean" onload="alert(1)`, "theme-ocean", "OCEAN"} {
		resp, body := saveStyle(t, app, creator, id, theme)
		if resp.StatusCode != http.StatusUnprocessableEntity {
			t.Errorf("theme %q: status = %d, want 422", theme, resp.StatusCode)
		}
		if !bodyContains(body, "Choose one of the themes offered") {
			t.Errorf("theme %q: no message says why it was refused:\n%s", theme, body)
		}
		if !regexp.MustCompile(`name="theme" value="slate" checked`).MatchString(body) {
			t.Errorf("theme %q: the Style tab no longer shows the draft's theme:\n%s", theme, body)
		}
	}
	if got := themeOf(t, mustGet(t, creator, app.Server.URL+"/surveys/"+id+"/preview")); got != "slate" {
		t.Errorf("after refused themes the draft's theme = %q, want slate", got)
	}
}

// TestStyle_CrossWorkspaceDenied: another workspace's survey has no
// Style tab to read and none to post to, and is left as it was.
func TestStyle_CrossWorkspaceDenied(t *testing.T) {
	t.Parallel()
	app := apptest.New(t, apptest.Options{})
	owner := app.Login(t, apptest.UniqueEmail("style-owner"))
	id := app.CreateSurvey(t, owner, "Somebody else's", true)
	app.AddQuestion(t, owner, id, "short_text", "Anything?", nil)
	intruder := app.Login(t, apptest.UniqueEmail("style-intruder"))

	resp, err := intruder.Get(app.Server.URL + "/surveys/" + id + "/style")
	if err != nil {
		t.Fatal(err)
	}
	body := apptest.ReadBody(t, resp)
	resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound || bodyContains(body, "Somebody else's") {
		t.Errorf("reading another workspace's Style tab: status %d\n%s", resp.StatusCode, body)
	}
	if resp, _ := saveStyle(t, app, intruder, id, "ocean"); resp.StatusCode != http.StatusNotFound {
		t.Errorf("styling another workspace's survey: status = %d, want 404", resp.StatusCode)
	}
	if got := themeOf(t, mustGet(t, owner, app.Server.URL+"/surveys/"+id+"/preview")); got != "" {
		t.Errorf("the owner's survey was styled by somebody else: theme %q", got)
	}
}

// TestStyle_InvitedParticipant: the personal link's pages are drawn in
// the survey's theme as the public ones are, the page that says an
// invitation was already answered among them.
func TestStyle_InvitedParticipant(t *testing.T) {
	t.Parallel()
	app := apptest.New(t, apptest.Options{})
	creator := app.Login(t, apptest.UniqueEmail("style-invite"))
	id := app.CreateSurvey(t, creator, "Invited style", false)
	app.AddQuestion(t, creator, id, "short_text", "Your team?", nil)
	saveStyle(t, app, creator, id, "forest")
	app.Publish(t, creator, id)
	addr := apptest.UniqueEmail("style-invitee")
	app.PostForm(t, creator, "/surveys/"+id+"/participants", url.Values{"emails": {addr}}).Body.Close()
	sendInvites(t, app, creator, id)
	link := inviteLinkTo(t, app, addr)
	path := link[strings.Index(link, "/p/"):]

	if got := themeOf(t, mustGet(t, jarClient(t), app.Server.URL+path)); got != "forest" {
		t.Errorf("the personal link's theme = %q, want forest", got)
	}
	if got := themeOf(t, answerOnce(t, app, path, "Platform")); got != "forest" {
		t.Errorf("the participant's thanks page theme = %q, want forest", got)
	}
	again := mustGet(t, jarClient(t), app.Server.URL+path)
	if !bodyContains(again, "already") {
		t.Fatalf("a second visit to an answered invitation is not the already answered page:\n%s", again)
	}
	if got := themeOf(t, again); got != "forest" {
		t.Errorf("the already answered page's theme = %q, want forest", got)
	}
	// The public address of an invited survey says it is by invitation,
	// in the survey's look.
	public, err := jarClient(t).Get(app.Server.URL + "/s/" + id)
	if err != nil {
		t.Fatal(err)
	}
	defer public.Body.Close()
	if got := themeOf(t, apptest.ReadBody(t, public)); got != "forest" {
		t.Errorf("the by invitation page's theme = %q, want forest", got)
	}
}

// TestStyle_Exported: the workspace export carries each version's style,
// and omits it where the version had Earful's own look.
func TestStyle_Exported(t *testing.T) {
	t.Parallel()
	app := apptest.New(t, apptest.Options{})
	creator := app.Login(t, apptest.UniqueEmail("style-export"))
	id := app.CreateSurvey(t, creator, "Exported style", true)
	app.AddQuestion(t, creator, id, "short_text", "Anything?", nil)
	app.Publish(t, creator, id)
	saveStyle(t, app, creator, id, "ocean")
	app.Publish(t, creator, id)

	link := waitForExport(t, app, creator)
	resp, err := creator.Get(app.Server.URL + link)
	if err != nil {
		t.Fatalf("download: %v", err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read archive: %v", err)
	}
	document := openArchive(t, raw)["workspace.json"]
	var archive export.Archive
	if err := json.Unmarshal(document, &archive); err != nil {
		t.Fatalf("workspace.json: %v", err)
	}
	if archive.FormatVersion != 7 {
		t.Errorf("format_version = %d, want 7, the version that added style", archive.FormatVersion)
	}
	// Read again without the types, to see the keys as an importer does.
	var loose struct {
		Surveys []struct {
			ID       string                       `json:"id"`
			Versions []map[string]json.RawMessage `json:"versions"`
		} `json:"surveys"`
	}
	if err := json.Unmarshal(document, &loose); err != nil {
		t.Fatalf("workspace.json: %v", err)
	}
	for _, survey := range loose.Surveys {
		if survey.ID != id {
			continue
		}
		if len(survey.Versions) != 2 {
			t.Fatalf("versions = %d, want 2", len(survey.Versions))
		}
		for _, version := range survey.Versions {
			style, has := version["style"]
			switch string(version["number"]) {
			case "1":
				if has {
					t.Errorf("version 1 had no style, exported %s", style)
				}
			case "2":
				var fields map[string]string
				if err := json.Unmarshal(style, &fields); err != nil || len(fields) != 1 || fields["theme"] != "ocean" {
					t.Errorf("version 2 style = %s, want only the theme, ocean", style)
				}
			}
		}
		return
	}
	t.Fatalf("the survey is missing from the export")
}

// TestThemeSheet_DevelopmentOnly: the theme sheet is a tool for whoever
// is changing the stylesheet. It is drawn in the theme its address
// names, falls back to the default for a name it does not know, and has
// no address outside development.
func TestThemeSheet_DevelopmentOnly(t *testing.T) {
	t.Parallel()
	app := apptest.New(t, apptest.Options{})
	for theme, want := range map[string]string{"": "", "earful": "", "slate": "slate", "ocean": "ocean", "forest": "forest", "nonsense": ""} {
		page := mustGet(t, jarClient(t), app.Server.URL+"/dev/theme-sheet?theme="+theme)
		if got := themeOf(t, page); got != want {
			t.Errorf("theme sheet for %q is drawn in %q, want %q", theme, got, want)
		}
		if !bodyContains(page, "Theme sheet") {
			t.Errorf("theme sheet for %q is not the sheet:\n%s", theme, page)
		}
	}

	staging := apptest.New(t, apptest.Options{Env: "staging"})
	resp, err := jarClient(t).Get(staging.Server.URL + "/dev/theme-sheet")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("the theme sheet outside development: status = %d, want 404", resp.StatusCode)
	}
}
