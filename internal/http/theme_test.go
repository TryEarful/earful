package http_test

import (
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/TryEarful/earful/internal/apptest"
)

// chooseTheme posts the theme switcher's form and returns the answer,
// without following its redirect.
func chooseTheme(t *testing.T, app *apptest.App, client *http.Client, theme, next string) *http.Response {
	t.Helper()
	resp, err := client.PostForm(app.Server.URL+"/theme", url.Values{"theme": {theme}, "next": {next}})
	if err != nil {
		t.Fatalf("POST /theme: %v", err)
	}
	resp.Body.Close()
	return resp
}

func themeCookieIn(resp *http.Response) *http.Cookie {
	for _, c := range resp.Cookies() {
		if c.Name == "theme" {
			return c
		}
	}
	return nil
}

// noRedirects makes a client stop at a redirect, so its target can be read.
func noRedirects(c *http.Client) *http.Client {
	c.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return c
}

const (
	lightChrome = `content="#F7F2E8" media="(prefers-color-scheme: light)"`
	darkChrome  = `content="#101823" media="(prefers-color-scheme: dark)"`
)

// TestTheme_FollowsTheSystemUntilChosen: with nothing chosen, a page
// names no theme, so the stylesheet follows the system, and the switcher
// says so.
func TestTheme_FollowsTheSystemUntilChosen(t *testing.T) {
	t.Parallel()
	app := apptest.New(t, apptest.Options{})
	_, page := reading(t, &http.Client{}, app.Server.URL+"/login", "en")
	for _, want := range []string{`action="/theme"`, `name="next" value="/login"`, `<option value="system" selected>`, lightChrome, darkChrome, "/static/js/theme.js"} {
		if !strings.Contains(page, want) {
			t.Errorf("the sign-in page lacks %s:\n%s", want, page)
		}
	}
	if strings.Contains(page, "data-theme=") {
		t.Errorf("a page with nothing chosen names a theme:\n%s", page)
	}
}

// TestTheme_TheChoiceIsRemembered: a theme chosen is kept in a cookie
// and drawn by the server on every page after, so the page arrives in
// it without a script.
func TestTheme_TheChoiceIsRemembered(t *testing.T) {
	t.Parallel()
	app := apptest.New(t, apptest.Options{})
	visitor := noRedirects(app.BrowserClient(t))

	resp := chooseTheme(t, app, visitor, "dark", "/login?notice=signed_out")
	if resp.StatusCode != http.StatusSeeOther || resp.Header.Get("Location") != "/login?notice=signed_out" {
		t.Fatalf("status %d, to %q", resp.StatusCode, resp.Header.Get("Location"))
	}
	cookie := themeCookieIn(resp)
	if cookie == nil || cookie.Value != "dark" || !cookie.HttpOnly || cookie.SameSite != http.SameSiteLaxMode || cookie.Path != "/" || cookie.Expires.IsZero() {
		t.Fatalf("cookie = %+v", cookie)
	}

	_, page := reading(t, visitor, app.Server.URL+"/login", "en")
	for _, want := range []string{
		`<html lang="en" data-theme="dark">`,
		`<option value="dark" selected>`,
		// The browser's chrome is dark whatever the system asks for.
		`content="#101823" media="(prefers-color-scheme: light)"`,
		darkChrome,
	} {
		if !strings.Contains(page, want) {
			t.Errorf("the page after choosing dark lacks %s:\n%s", want, page)
		}
	}

	chooseTheme(t, app, visitor, "light", "/")
	_, page = reading(t, visitor, app.Server.URL+"/trust", "en")
	for _, want := range []string{`data-theme="light"`, lightChrome, `content="#F7F2E8" media="(prefers-color-scheme: dark)"`} {
		if !strings.Contains(page, want) {
			t.Errorf("a document after choosing light lacks %s:\n%s", want, page)
		}
	}

	// Following the system forgets the choice.
	resp = chooseTheme(t, app, visitor, "system", "/login")
	if c := themeCookieIn(resp); c == nil || c.MaxAge >= 0 {
		t.Fatalf("following the system did not remove the cookie: %+v", c)
	}
	_, page = reading(t, visitor, app.Server.URL+"/login", "en")
	if strings.Contains(page, "data-theme=") || !strings.Contains(page, lightChrome) || !strings.Contains(page, darkChrome) {
		t.Errorf("the page after following the system again:\n%s", page)
	}
}

// TestTheme_OnARespondentsPage: a respondent may choose a theme too,
// on the survey itself, and is brought back to it.
func TestTheme_OnARespondentsPage(t *testing.T) {
	t.Parallel()
	app := apptest.New(t, apptest.Options{})
	creator := app.Login(t, apptest.UniqueEmail("theme"))
	id := app.CreateSurvey(t, creator, "In the dark", true)
	app.AddQuestion(t, creator, id, "long_text", "How was it?", nil)
	app.Publish(t, creator, id)
	share := "/s/" + id + "?lang=en"

	respondent := noRedirects(app.BrowserClient(t))
	_, page := reading(t, respondent, app.Server.URL+share, "en")
	if !strings.Contains(page, `action="/theme"`) || strings.Contains(page, "data-theme=") {
		t.Fatalf("the survey has no theme switcher, or names a theme unasked:\n%s", page)
	}

	resp := chooseTheme(t, app, respondent, "dark", share)
	if resp.Header.Get("Location") != share {
		t.Errorf("the respondent was sent to %q, not back to the survey", resp.Header.Get("Location"))
	}
	_, page = reading(t, respondent, app.Server.URL+share, "en")
	if !strings.Contains(page, `data-theme="dark"`) || !bodyContains(page, "How was it?") {
		t.Errorf("the survey after choosing dark:\n%s", page)
	}
}

// TestTheme_WhatIsNotAThemeChangesNothing: a value the switcher does not
// offer is not remembered, a cookie holding one is passed over, and a
// place that is not on this site is not gone to.
func TestTheme_WhatIsNotAThemeChangesNothing(t *testing.T) {
	t.Parallel()
	app := apptest.New(t, apptest.Options{})
	visitor := noRedirects(app.BrowserClient(t))

	for _, theme := range []string{"", "purple", "Dark", `dark" onload="x`} {
		resp := chooseTheme(t, app, visitor, theme, "/login")
		if len(resp.Cookies()) != 0 || resp.Header.Get("Location") != "/login" {
			t.Errorf("%q: cookies %v, to %q", theme, resp.Cookies(), resp.Header.Get("Location"))
		}
	}
	for _, next := range []string{"https://evil.example/", "//evil.example/", "/\\evil.example", "login"} {
		if resp := chooseTheme(t, app, visitor, "dark", next); resp.Header.Get("Location") != "/" {
			t.Errorf("next %q sent the visitor to %q", next, resp.Header.Get("Location"))
		}
	}

	stranger := app.BrowserClient(t)
	setRawCookie(t, stranger, app.Server.URL, "theme", "purple")
	_, page := reading(t, stranger, app.Server.URL+"/login", "en")
	if strings.Contains(page, "data-theme=") || !strings.Contains(page, `<option value="system" selected>`) {
		t.Errorf("a cookie holding no theme was drawn:\n%s", page)
	}
}
