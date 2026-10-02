package http_test

import (
	"net/http"
	"strings"
	"testing"

	"github.com/TryEarful/earful/internal/apptest"
)

// TestDocuments_AreServedAtTheirAddress: a document's place in web/pages
// is its address, and a reader needs no session to read one.
func TestDocuments_AreServedAtTheirAddress(t *testing.T) {
	t.Parallel()
	app := apptest.New(t, apptest.Options{})
	anyone := &http.Client{}

	help := mustGet(t, anyone, app.Server.URL+"/help/voice")
	for _, want := range []string{"Answering by voice", "never stored", "Last updated"} {
		if !bodyContains(help, want) {
			t.Errorf("the help page does not say %q:\n%s", want, help)
		}
	}
	if !strings.Contains(help, "<title>Answering by voice · Earful</title>") {
		t.Errorf("the window is not titled for the document:\n%s", help[:300])
	}
	// Rendered, not shown as it was typed. The Markdown is on the page as
	// well, for copying, so its absence is not what is looked for.
	if !strings.Contains(help, "<strong>Dictate</strong>") {
		t.Errorf("the Markdown was not rendered:\n%s", help)
	}
}

// TestDocuments_CanBeTakenAwayAsMarkdown: the link works with no script,
// and what it serves is the document with this instance's facts in it,
// not the document's machinery.
func TestDocuments_CanBeTakenAwayAsMarkdown(t *testing.T) {
	t.Parallel()
	app := apptest.New(t, apptest.Options{
		HostingRegion: "a rack in Utrecht",
		ContactEmail:  "privacy@example.org",
	})
	anyone := &http.Client{}

	page := mustGet(t, anyone, app.Server.URL+"/trust")
	if !strings.Contains(page, `href="/trust.md"`) {
		t.Errorf("the page has no link to its Markdown:\n%s", page)
	}
	if !strings.Contains(page, "data-copy-markdown hidden") {
		t.Errorf("the copy button is not drawn hidden for the script to show:\n%s", page)
	}

	resp, err := anyone.Get(app.Server.URL + "/trust.md")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()
	if got := resp.Header.Get("Content-Type"); !strings.HasPrefix(got, "text/markdown") {
		t.Errorf("Content-Type = %q", got)
	}
	markdown := apptest.ReadBody(t, resp)
	for _, want := range []string{
		"title: How Earful treats your data",
		"last_update: ",
		"hash: sha256-",
		"# How Earful treats your data",
		"## Your voice is never stored",
		"Hosted in a rack in Utrecht.",
		"(mailto:privacy@example.org)",
		"No outside company is involved: this instance runs entirely on its operator's own infrastructure.",
	} {
		if !strings.Contains(markdown, want) {
			t.Errorf("the Markdown does not contain %q:\n%s", want, markdown)
		}
	}
	for _, not := range []string{"{{", "Brevo", "<p>"} {
		if strings.Contains(markdown, not) {
			t.Errorf("the Markdown contains %q:\n%s", not, markdown)
		}
	}
}

// TestDocuments_ADraftIsServedOnlyInDevelopment: a document still being
// written has an address where its writer works and nowhere else. A
// notice that says "not written yet" is not one anyone should be shown,
// and terms nobody has reviewed are not terms anyone has agreed to.
func TestDocuments_ADraftIsServedOnlyInDevelopment(t *testing.T) {
	t.Parallel()
	anyone := &http.Client{}

	writing := apptest.New(t, apptest.Options{})
	if page := mustGet(t, anyone, writing.Server.URL+"/privacy"); !bodyContains(page, "has not been written yet") {
		t.Errorf("a draft is not served to its writer:\n%s", page)
	}

	released := apptest.New(t, apptest.Options{Env: "staging"})
	for _, address := range []string{"/terms", "/terms.md", "/privacy", "/privacy.md"} {
		resp, err := anyone.Get(released.Server.URL + address)
		if err != nil {
			t.Fatalf("GET %s: %v", address, err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusNotFound {
			t.Errorf("%s = %d on a released instance, want 404", address, resp.StatusCode)
		}
	}
	// What is finished is served there as anywhere.
	if page := mustGet(t, anyone, released.Server.URL+"/trust"); !bodyContains(page, "Your voice is never stored") {
		t.Errorf("the trust page is not served on a released instance:\n%s", page)
	}
}
