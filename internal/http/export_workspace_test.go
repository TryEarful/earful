package http_test

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/TryEarful/earful/internal/ai"
	"github.com/TryEarful/earful/internal/apptest"
	"github.com/TryEarful/earful/internal/export"
)

// waitForExport presses the export button and polls the account page the
// way a person would, until the archive is ready.
func waitForExport(t *testing.T, app *apptest.App, client *http.Client) string {
	t.Helper()
	app.PostForm(t, client, "/account/export", nil).Body.Close()

	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		page := mustGet(t, client, app.Server.URL+"/account")
		if link := exportLink(page); link != "" {
			return link
		}
		if bodyContains(page, "too large to export") || bodyContains(page, "Something went wrong building") {
			t.Fatalf("export failed:\n%s", page)
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatal("the export never became ready")
	return ""
}

func exportLink(page string) string {
	const marker = `href="/exports/`
	i := strings.Index(page, marker)
	if i < 0 {
		return ""
	}
	rest := page[i+len(`href="`):]
	return rest[:strings.Index(rest, `"`)]
}

func openArchive(t *testing.T, body []byte) map[string][]byte {
	t.Helper()
	reader, err := zip.NewReader(bytes.NewReader(body), int64(len(body)))
	if err != nil {
		t.Fatalf("open zip: %v", err)
	}
	files := map[string][]byte{}
	for _, file := range reader.File {
		rc, err := file.Open()
		if err != nil {
			t.Fatalf("open %s: %v", file.Name, err)
		}
		content, err := io.ReadAll(rc)
		rc.Close()
		if err != nil {
			t.Fatalf("read %s: %v", file.Name, err)
		}
		files[file.Name] = content
	}
	return files
}

// TestWorkspaceExport_ContainsEverythingTheWorkspaceHolds is story 59 and
// M7-T3's AC: a seeded workspace round-trips against the documented
// format, and the download link expires.
func TestWorkspaceExport_ContainsEverythingTheWorkspaceHolds(t *testing.T) {
	t.Parallel()
	app := apptest.New(t, apptest.Options{})
	creator := app.Login(t, apptest.UniqueEmail("export"))

	// An anonymous survey, published twice, answered under both versions.
	anon := app.CreateSurvey(t, creator, "Anonymous survey", true)
	app.AddQuestion(t, creator, anon, "long_text", "What stood out?", nil)
	app.AddQuestion(t, creator, anon, "rating_scale", "How was it?", url.Values{
		"scale_min": {"1"}, "scale_max": {"7"},
	})
	app.Publish(t, creator, anon)
	answerSurvey(t, app, anon, map[int]string{0: "First version answer", 1: "7"})

	identity := app.QuestionIdentities(t, creator, anon)[0]
	app.PostForm(t, creator, "/surveys/"+anon+"/questions/"+identity, url.Values{
		"type": {"long_text"}, "text": {"Looking back, what stood out?"},
	}).Body.Close()
	app.Publish(t, creator, anon)
	answerSurvey(t, app, anon, map[int]string{0: "Second version answer", 1: "3"})

	// An invited survey with a participant.
	invited := app.CreateSurvey(t, creator, "Invited survey", false)
	app.AddQuestion(t, creator, invited, "short_text", "Your take?", nil)
	app.Publish(t, creator, invited)
	guest := apptest.UniqueEmail("guest")
	app.PostForm(t, creator, "/surveys/"+invited+"/participants", url.Values{"emails": {guest}}).Body.Close()
	app.PostForm(t, creator, "/surveys/"+invited+"/participants/send", nil).Body.Close()

	link := waitForExport(t, app, creator)

	resp, err := creator.Get(app.Server.URL + link)
	if err != nil {
		t.Fatalf("download: %v", err)
	}
	defer resp.Body.Close()
	if got := resp.Header.Get("Content-Type"); got != "application/zip" {
		t.Errorf("Content-Type = %q", got)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read archive: %v", err)
	}
	files := openArchive(t, body)

	if _, ok := files["workspace.json"]; !ok {
		t.Fatalf("no workspace.json in the archive; files: %v", keys(files))
	}
	if _, ok := files["README.txt"]; !ok {
		t.Errorf("no README.txt: an archive should explain itself; files: %v", keys(files))
	}
	var csvCount int
	for name := range files {
		if strings.HasPrefix(name, "surveys/") && strings.HasSuffix(name, ".csv") {
			csvCount++
		}
	}
	if csvCount != 2 {
		t.Errorf("archive holds %d survey CSVs, want one per survey", csvCount)
	}

	var archive export.Archive
	if err := json.Unmarshal(files["workspace.json"], &archive); err != nil {
		t.Fatalf("workspace.json does not match the documented format: %v", err)
	}
	if archive.FormatVersion != export.FormatVersion {
		t.Errorf("format_version = %d, want %d", archive.FormatVersion, export.FormatVersion)
	}
	if len(archive.Surveys) != 2 {
		t.Fatalf("archive holds %d surveys, want 2", len(archive.Surveys))
	}

	var anonymous, invitedSurvey *export.Survey
	for i := range archive.Surveys {
		switch archive.Surveys[i].Title {
		case "Anonymous survey":
			anonymous = &archive.Surveys[i]
		case "Invited survey":
			invitedSurvey = &archive.Surveys[i]
		}
	}
	if anonymous == nil || invitedSurvey == nil {
		t.Fatalf("surveys missing from the archive: %+v", archive.Surveys)
	}

	// Both versions travel, with their questions and the identity that
	// ties them together — the thing an importer must not lose.
	if len(anonymous.Versions) != 2 {
		t.Fatalf("anonymous survey exported %d versions, want 2", len(anonymous.Versions))
	}
	first, second := anonymous.Versions[0].Questions[0], anonymous.Versions[1].Questions[0]
	if first.IdentityID != second.IdentityID {
		t.Error("the same question exported with different identities across versions")
	}
	if first.Text == second.Text {
		t.Error("the rewording did not survive the export")
	}
	// The rating scale's bounds travel too, so a reader knows what a 7
	// meant.
	if anonymous.Versions[0].Questions[1].ScaleMax != 7 {
		t.Errorf("scale bounds lost: %+v", anonymous.Versions[0].Questions[1])
	}

	// Responses carry the version they answered and their answers keyed
	// by identity.
	if len(anonymous.Responses) != 2 {
		t.Fatalf("anonymous survey exported %d responses, want 2", len(anonymous.Responses))
	}
	for _, response := range anonymous.Responses {
		if response.ParticipantEmail != nil {
			t.Error("an anonymous response exported a participant email")
		}
		if len(response.Answers) == 0 {
			t.Error("a response exported no answers")
		}
	}
	if anonymous.Responses[0].Version == anonymous.Responses[1].Version {
		t.Error("responses did not keep the different versions they answered")
	}

	// Invited surveys export their participants; anonymous ones have
	// none to export.
	if len(invitedSurvey.Participants) != 1 || invitedSurvey.Participants[0].Email != guest {
		t.Errorf("invited participants missing: %+v", invitedSurvey.Participants)
	}
	if len(anonymous.Participants) != 0 {
		t.Error("an anonymous survey exported participants")
	}

	// And nothing identifying rode along with the anonymous answers.
	raw := string(files["workspace.json"])
	for _, forbidden := range []string{"ip", "user_agent", "useragent"} {
		if strings.Contains(strings.ToLower(raw), `"`+forbidden+`"`) {
			t.Errorf("export contains a %q field near responses", forbidden)
		}
	}
}

// TestWorkspaceExport_CarriesInsightsLabelled is M10-T2's export half:
// a summary travels, and it travels as analysis rather than as data.
func TestWorkspaceExport_CarriesInsightsLabelled(t *testing.T) {
	t.Parallel()
	fake := &ai.Fake{AnalyzeScript: [][]string{{"Themes: onboarding friction."}}}
	app := apptest.New(t, apptest.Options{AI: fake})
	creator := app.Login(t, apptest.UniqueEmail("export-insight"))

	id := app.CreateSurvey(t, creator, "Insightful survey", true)
	app.AddQuestion(t, creator, id, "long_text", "How was it?", nil)
	app.Publish(t, creator, id)
	answerSurvey(t, app, id, map[int]string{0: "Onboarding was confusing"})
	app.PostForm(t, creator, "/surveys/"+id+"/insights", nil).Body.Close()

	link := waitForExport(t, app, creator)
	resp, err := creator.Get(app.Server.URL + link)
	if err != nil {
		t.Fatalf("download: %v", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read archive: %v", err)
	}
	files := openArchive(t, body)

	var archive export.Archive
	if err := json.Unmarshal(files["workspace.json"], &archive); err != nil {
		t.Fatalf("workspace.json: %v", err)
	}
	if len(archive.Surveys) != 1 || len(archive.Surveys[0].Insights) != 1 {
		t.Fatalf("no insight in the export: %+v", archive.Surveys)
	}
	insight := archive.Surveys[0].Insights[0]
	if !strings.Contains(insight.Output, "onboarding friction") {
		t.Errorf("exported summary = %q", insight.Output)
	}
	if insight.Note == "" || insight.Model == "" {
		t.Errorf("exported summary is not labelled: %+v", insight)
	}

	// And a human opening the zip finds it beside the CSV, labelled.
	var sidecar string
	for name, content := range files {
		if strings.HasSuffix(name, ".insight.txt") {
			sidecar = string(content)
		}
	}
	if sidecar == "" {
		t.Fatalf("no insight sidecar in the archive; files: %v", keys(files))
	}
	if !strings.Contains(sidecar, "AI-generated summary") || !strings.Contains(sidecar, "Model:") {
		t.Errorf("the sidecar does not label itself:\n%s", sidecar)
	}
}

// TestWorkspaceExport_IsScopedAndExpires: another workspace cannot
// download it, and neither can anyone once it has expired.
func TestWorkspaceExport_IsScopedAndExpires(t *testing.T) {
	t.Parallel()
	app := apptest.New(t, apptest.Options{})
	owner := app.Login(t, apptest.UniqueEmail("exportowner"))
	stranger := app.Login(t, apptest.UniqueEmail("exportstranger"))

	id := app.CreateSurvey(t, owner, "Private", true)
	app.AddQuestion(t, owner, id, "short_text", "Secret question", nil)
	app.Publish(t, owner, id)
	link := waitForExport(t, app, owner)

	resp, err := stranger.Get(app.Server.URL + link)
	if err != nil {
		t.Fatalf("stranger download: %v", err)
	}
	body := apptest.ReadBody(t, resp)
	resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("stranger got status %d, want 404", resp.StatusCode)
	}
	if strings.Contains(body, "Secret question") {
		t.Error("another workspace's export was served")
	}

	// A day later the link is gone, for the owner too.
	app.Clock.Advance(25 * time.Hour)
	resp, err = owner.Get(app.Server.URL + link)
	if err != nil {
		t.Fatalf("owner download after expiry: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("expired link status = %d, want 404", resp.StatusCode)
	}
}

func keys(m map[string][]byte) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
