package http_test

import (
	"html"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/TryEarful/earful/internal/apptest"
)

// bodyContains reports whether a page shows want the way a reader sees
// it: entities are unescaped first, so an assertion for "sam's workspace"
// matches the "sam&#39;s workspace" templ correctly emits.
func bodyContains(body, want string) bool {
	return strings.Contains(html.UnescapeString(body), want)
}

// Cookie-jar helpers shared by the auth/session tests: they let a test
// inspect or plant individual cookies while still driving the server
// through an ordinary browser-like client.

func mustParseURL(t *testing.T, raw string) *url.URL {
	t.Helper()
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatalf("parse url %q: %v", raw, err)
	}
	return u
}

func setRawCookie(t *testing.T, client *http.Client, serverURL, name, value string) {
	t.Helper()
	u := mustParseURL(t, serverURL)
	client.Jar.SetCookies(u, []*http.Cookie{{Name: name, Value: value, Path: "/"}})
}

func cookieValue(t *testing.T, client *http.Client, serverURL, name string) string {
	t.Helper()
	for _, c := range client.Jar.Cookies(mustParseURL(t, serverURL)) {
		if c.Name == name {
			return c.Value
		}
	}
	t.Fatalf("no %q cookie in jar", name)
	return ""
}

// --- respondent-page helpers ---------------------------------------------

func mustGet(t *testing.T, client *http.Client, url string) string {
	t.Helper()
	resp, err := client.Get(url)
	if err != nil {
		t.Fatalf("GET %s: %v", url, err)
	}
	defer resp.Body.Close()
	return apptest.ReadBody(t, resp)
}

var versionFieldRe = regexp.MustCompile(`name="version_id" value="([0-9a-f-]{36})"`)

// extractVersionID reads the version the form was rendered from — the
// same thing a browser posts back, which is what pins the response.
func extractVersionID(t *testing.T, page string) string {
	t.Helper()
	m := versionFieldRe.FindStringSubmatch(page)
	if m == nil {
		t.Fatalf("no version_id field on the page:\n%s", page)
	}
	return m[1]
}

var answerFieldRe = regexp.MustCompile(`name="q_([0-9a-f-]{36})"`)

// extractAnswerFields returns each question's field identity in the order
// they appear, so tests can fill the form the way a browser would.
func extractAnswerFields(t *testing.T, page string) []string {
	t.Helper()
	seen := map[string]bool{}
	var ids []string
	for _, m := range answerFieldRe.FindAllStringSubmatch(page, -1) {
		if !seen[m[1]] {
			seen[m[1]] = true
			ids = append(ids, m[1])
		}
	}
	if len(ids) == 0 {
		t.Fatalf("no answer fields on the page:\n%s", page)
	}
	return ids
}

var (
	formTsRe = regexp.MustCompile(`name="form_ts" value="([^"]+)"`)
	nonceRe  = regexp.MustCompile(`name="form_nonce" value="([^"]+)"`)
)

// respondForm extracts the form's own fields (version, signed timestamp,
// nonce) from a rendered respondent page, exactly as a browser would
// submit them. Tests add answers on top and advance the fake clock past
// the minimum fill time before posting.
func respondForm(t *testing.T, page string) url.Values {
	t.Helper()
	form := url.Values{}
	form.Set("version_id", extractVersionID(t, page))
	if m := formTsRe.FindStringSubmatch(page); m != nil {
		form.Set("form_ts", m[1])
	}
	if m := nonceRe.FindStringSubmatch(page); m != nil {
		form.Set("form_nonce", m[1])
	}
	return form
}

// submitAfterReading posts a respondent form the way a person does: some
// seconds after it was rendered. The fake clock provides the reading time.
func submitAfterReading(t *testing.T, app *apptest.App, client *http.Client, surveyID string, form url.Values) (*http.Response, string) {
	t.Helper()
	return submitAfterReadingTo(t, app, client, "/s/"+surveyID, form)
}

// submitAfterReadingTo is the path-generic form, for participant links.
func submitAfterReadingTo(t *testing.T, app *apptest.App, client *http.Client, path string, form url.Values) (*http.Response, string) {
	t.Helper()
	app.Clock.Advance(5 * time.Second)
	resp, err := client.PostForm(app.Server.URL+path, form)
	if err != nil {
		t.Fatalf("POST %s: %v", path, err)
	}
	body := apptest.ReadBody(t, resp)
	resp.Body.Close()
	return resp, body
}

var urlAttrRe = regexp.MustCompile(`(?:src|href|action)="([^"]*)"`)

// findExternalURLs returns every referenced URL that is not same-origin
// relative — the mechanical form of ADR-0006's "no third-party requests
// on respondent pages".
func findExternalURLs(page string) []string {
	var external []string
	for _, m := range urlAttrRe.FindAllStringSubmatch(page, -1) {
		ref := m[1]
		switch {
		case ref == "", strings.HasPrefix(ref, "/"), strings.HasPrefix(ref, "#"):
			// Same-origin absolute path or in-page anchor.
		default:
			external = append(external, ref)
		}
	}
	return external
}
