package http_test

import (
	"encoding/json"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"testing"

	"golang.org/x/sync/semaphore"

	"github.com/TryEarful/earful/internal/apptest"
	"github.com/TryEarful/earful/internal/export"
)

// The account's style (ADR-0023): the theme, header, footer and thanks
// picture a workspace's surveys follow until they make a part their own,
// set on a page of the account.

func postAccountStyle(t *testing.T, app *apptest.App, creator *http.Client, form url.Values, files ...apptest.Upload) (*http.Response, string) {
	t.Helper()
	resp := app.PostMultipart(t, creator, "/account/style", form, files...)
	defer resp.Body.Close()
	return resp, apptest.ReadBody(t, resp)
}

var currentPictureRe = regexp.MustCompile(`<img[^>]*\bjs-style-image-current\b[^>]*>`)

// accountStyleForm is the account's style form with every part filled
// and a thanks picture chosen, as the page posts it.
func accountStyleForm(alt string) url.Values {
	form := pictureForm(alt)
	form.Set("thanks_picture", "confetti")
	form.Set("thanks_alt", "")
	return form
}

// TestAccountStyle_SavedAndShownToItsOwner: an account's style is saved
// part by part and shown again on its page, with its picture served from
// an address only its own creator can fetch. No survey changes with it.
func TestAccountStyle_SavedAndShownToItsOwner(t *testing.T) {
	t.Parallel()
	app := apptest.New(t, apptest.Options{})
	creator := app.Login(t, apptest.UniqueEmail("account-style"))
	live := publishedSurvey(t, app, creator, "Live before", true)

	page := mustGet(t, creator, app.Server.URL+"/account/style")
	if !bodyContains(page, "js-account-style-form") || !bodyContains(page, "Save account style") {
		t.Fatalf("the account's style page has no form:\n%s", page)
	}

	resp, page := postAccountStyle(t, app, creator, accountStyleForm("Corner Workshop"), ownLogo(t))
	if resp.StatusCode != http.StatusOK || !bodyContains(page, "Account style saved") {
		t.Fatalf("saving: status %d\n%s", resp.StatusCode, page)
	}
	// The open surveys that follow the account's style are asked about
	// first; the style itself is on its page.
	page = mustGet(t, creator, app.Server.URL+"/account/style")
	for _, want := range []string{"Corner Workshop", "Evening classes in wood and clay.", "https://example.com/privacy", "Privacy notice"} {
		if !bodyContains(page, want) {
			t.Errorf("the saved style does not show %q", want)
		}
	}
	if !regexp.MustCompile(`value="ocean" checked`).MatchString(page) {
		t.Errorf("the saved theme is not the one chosen")
	}
	if !regexp.MustCompile(`value="confetti" checked`).MatchString(page) {
		t.Errorf("the saved thanks picture is not the one chosen")
	}

	src := pictureSrc(page, currentPictureRe)
	if !strings.HasPrefix(src, "/account/style-image/") {
		t.Fatalf("the logo is shown from %q, want the account's own address", src)
	}
	resp, body := fetch(t, creator, app.Server.URL+src)
	if resp.StatusCode != http.StatusOK || resp.Header.Get("Content-Type") != "image/png" || len(body) == 0 {
		t.Fatalf("the owner's logo: status %d, type %q", resp.StatusCode, resp.Header.Get("Content-Type"))
	}
	if cache := resp.Header.Get("Cache-Control"); cache != "private, no-cache" {
		t.Errorf("the account's picture is cached as %q", cache)
	}
	stranger := app.Login(t, apptest.UniqueEmail("account-style-stranger"))
	if resp, _ := fetch(t, stranger, app.Server.URL+src); resp.StatusCode != http.StatusNotFound {
		t.Errorf("another workspace fetched the account's picture: status %d", resp.StatusCode)
	}
	if resp, _ := fetch(t, jarClient(t), app.Server.URL+src); resp.Header.Get("Content-Type") == "image/png" {
		t.Errorf("somebody without a session fetched the account's picture")
	}
	if resp, _ := fetch(t, jarClient(t), app.Server.URL+"/style-image/"+strings.TrimPrefix(src, "/account/style-image/")); resp.StatusCode != http.StatusNotFound {
		t.Errorf("the public address serves an account's picture: status %d", resp.StatusCode)
	}
	if strangerPage := mustGet(t, stranger, app.Server.URL+"/account/style"); bodyContains(strangerPage, "Corner Workshop") {
		t.Errorf("another workspace's style page shows this account's style")
	}

	// A published survey keeps the look it was published with.
	if respondent := mustGet(t, jarClient(t), app.Server.URL+"/s/"+live); bodyContains(respondent, "Corner Workshop") {
		t.Errorf("a published survey took the account's style before being published again")
	}
}

// TestAccountStyle_APartLeftOutIsKept: a form that carries only the theme
// changes the theme and keeps the header, footer and pictures.
func TestAccountStyle_APartLeftOutIsKept(t *testing.T) {
	t.Parallel()
	app := apptest.New(t, apptest.Options{})
	creator := app.Login(t, apptest.UniqueEmail("account-style-parts"))
	if resp, page := postAccountStyle(t, app, creator, accountStyleForm("Corner Workshop"), ownLogo(t)); resp.StatusCode != http.StatusOK {
		t.Fatalf("saving: status %d\n%s", resp.StatusCode, page)
	}
	resp, page := postAccountStyle(t, app, creator, url.Values{"theme": {"forest"}})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("saving the theme: status %d\n%s", resp.StatusCode, page)
	}
	page = mustGet(t, creator, app.Server.URL+"/account/style")
	if !regexp.MustCompile(`value="forest" checked`).MatchString(page) {
		t.Errorf("the theme did not change")
	}
	if !bodyContains(page, "Corner Workshop Cooperative") || pictureSrc(page, currentPictureRe) == "" {
		t.Errorf("saving the theme alone lost the footer or the logo:\n%s", page)
	}
}

// TestAccountStyle_RefusedKeepsWhatWasTyped: a style that cannot be
// saved is shown again as typed, with the problem beside its field, and
// the account's style stays as it was.
func TestAccountStyle_RefusedKeepsWhatWasTyped(t *testing.T) {
	t.Parallel()
	app := apptest.New(t, apptest.Options{})
	creator := app.Login(t, apptest.UniqueEmail("account-style-refused"))
	form := accountStyleForm("")
	form.Set("header_name", "Typed and refused")
	form["header_link_url"] = []string{"javascript:alert(1)", "", "https://example.com/contact"}

	resp, page := postAccountStyle(t, app, creator, form)
	if resp.StatusCode != http.StatusUnprocessableEntity {
		t.Fatalf("a bad link: status %d\n%s", resp.StatusCode, page)
	}
	if !bodyContains(page, "Typed and refused") || !bodyContains(page, "Header link 1") || !bodyContains(page, "js-style-field-error") {
		t.Errorf("the refused form is not shown as typed with its problem:\n%s", page)
	}
	if again := mustGet(t, creator, app.Server.URL+"/account/style"); bodyContains(again, "Typed and refused") {
		t.Errorf("a refused style was saved")
	}
}

// TestAccountStyle_ReplacedPicturesMakeRoom: pictures the account's
// style no longer shows make room for new ones, so a creator can change
// the logo as often as they like.
func TestAccountStyle_ReplacedPicturesMakeRoom(t *testing.T) {
	t.Parallel()
	app := apptest.New(t, apptest.Options{})
	creator := app.Login(t, apptest.UniqueEmail("account-style-room"))
	for n := 1; n <= 12; n++ {
		if resp, page := postAccountStyle(t, app, creator, accountStyleForm("Logo"), ownLogo(t)); resp.StatusCode != http.StatusOK {
			t.Fatalf("logo %d: status %d\n%s", n, resp.StatusCode, page)
		}
	}
}

// TestAccountStyle_ASuspendedWorkspaceCanSave: a suspension stops what
// reaches respondents and what costs money; the account's style does
// neither until a survey is published, which a suspension refuses.
func TestAccountStyle_ASuspendedWorkspaceCanSave(t *testing.T) {
	t.Parallel()
	app := apptest.New(t, apptest.Options{})
	address := apptest.UniqueEmail("account-style-suspended")
	creator := app.Login(t, address)
	suspendWorkspace(t, app, address, "Impersonating a bank")
	if resp, page := postAccountStyle(t, app, creator, accountStyleForm("Corner Workshop")); resp.StatusCode != http.StatusOK {
		t.Fatalf("saving while suspended: status %d\n%s", resp.StatusCode, page)
	}
}

// TestAccountStyle_BusyLeadsBack: while as many style forms are being
// handled as the instance allows, the account's is turned away with a
// way back to its page.
func TestAccountStyle_BusyLeadsBack(t *testing.T) {
	t.Parallel()
	full := semaphore.NewWeighted(1)
	if !full.TryAcquire(1) {
		t.Fatal("could not fill the bound")
	}
	app := apptest.New(t, apptest.Options{StyleSaves: full})
	creator := app.Login(t, apptest.UniqueEmail("account-style-busy"))
	resp, page := postAccountStyle(t, app, creator, accountStyleForm("A logo"), ownLogo(t))
	if resp.StatusCode != http.StatusServiceUnavailable || !bodyContains(page, `href="/account/style"`) {
		t.Fatalf("status %d, want 503 leading back to the account's style\n%s", resp.StatusCode, page)
	}
}

// TestAccountStyle_Exported: the workspace export carries the account's
// style on the workspace, and the pictures it shows under images/.
func TestAccountStyle_Exported(t *testing.T) {
	t.Parallel()
	app := apptest.New(t, apptest.Options{})
	creator := app.Login(t, apptest.UniqueEmail("account-style-export"))

	// With no account style, the workspace carries none.
	plain := app.Login(t, apptest.UniqueEmail("account-style-export-none"))
	if archive, _ := exportedWorkspace(t, app, plain); archive.Workspace.Style != nil {
		t.Fatalf("a workspace with no account style exported one: %+v", archive.Workspace.Style)
	}

	logo := ownLogo(t)
	if resp, page := postAccountStyle(t, app, creator, accountStyleForm("Corner Workshop"), logo); resp.StatusCode != http.StatusOK {
		t.Fatalf("saving: status %d\n%s", resp.StatusCode, page)
	}
	archive, files := exportedWorkspace(t, app, creator)
	if archive.FormatVersion != export.FormatVersion || export.FormatVersion != 8 {
		t.Errorf("format version %d, want 8", archive.FormatVersion)
	}
	style := archive.Workspace.Style
	if style == nil || style.Theme != "ocean" || style.Header == nil || style.Header.Name != "Corner Workshop" ||
		style.Footer == nil || style.Thanks == nil || style.Thanks.Picture != "confetti" {
		t.Fatalf("the account's style was exported as %+v", style)
	}
	if style.Header.Logo == nil || style.Header.Logo.Alt != "Corner Workshop" {
		t.Fatalf("the account's logo was exported as %+v", style.Header.Logo)
	}
	if _, ok := files[style.Header.Logo.File]; !ok {
		t.Errorf("the archive does not hold %s", style.Header.Logo.File)
	}
}

// exportedWorkspace builds the creator's workspace export and reads it.
func exportedWorkspace(t *testing.T, app *apptest.App, creator *http.Client) (export.Archive, map[string][]byte) {
	t.Helper()
	link := waitForExport(t, app, creator)
	resp, raw := fetch(t, creator, app.Server.URL+link)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("download: status %d", resp.StatusCode)
	}
	files := openArchive(t, raw)
	var archive export.Archive
	if err := json.Unmarshal(files["workspace.json"], &archive); err != nil {
		t.Fatalf("workspace.json: %v", err)
	}
	return archive, files
}
