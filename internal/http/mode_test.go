package http_test

import (
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/TryEarful/earful/internal/apptest"
)

// chooseMode posts the display mode switcher's form and returns the answer,
// without following its redirect.
func chooseMode(t *testing.T, app *apptest.App, client *http.Client, mode, next string) *http.Response {
	t.Helper()
	resp, err := client.PostForm(app.Server.URL+"/mode", url.Values{"mode": {mode}, "next": {next}})
	if err != nil {
		t.Fatalf("POST /mode: %v", err)
	}
	resp.Body.Close()
	return resp
}

// cookieIn is the cookie of that name an answer sets, if it sets one.
func cookieIn(resp *http.Response, name string) *http.Cookie {
	for _, c := range resp.Cookies() {
		if c.Name == name {
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

// TestMode_FollowsTheSystemUntilChosen: with nothing chosen, a page
// names no display mode, so the stylesheet follows the system, and the
// switcher says so.
func TestMode_FollowsTheSystemUntilChosen(t *testing.T) {
	t.Parallel()
	app := apptest.New(t, apptest.Options{})
	_, page := reading(t, &http.Client{}, app.Server.URL+"/login", "en")
	for _, want := range []string{`action="/mode"`, `name="next" value="/login"`, `<option value="system" selected>`, lightChrome, darkChrome, "/static/js/mode.js"} {
		if !strings.Contains(page, want) {
			t.Errorf("the sign-in page lacks %s:\n%s", want, page)
		}
	}
	if strings.Contains(page, "data-mode=") {
		t.Errorf("a page with nothing chosen names a display mode:\n%s", page)
	}
}

// TestMode_TheChoiceIsRemembered: a display mode chosen is kept in a
// cookie and drawn by the server on every page after, so the page arrives in
// it without a script.
func TestMode_TheChoiceIsRemembered(t *testing.T) {
	t.Parallel()
	app := apptest.New(t, apptest.Options{})
	visitor := noRedirects(app.BrowserClient(t))

	resp := chooseMode(t, app, visitor, "dark", "/login?notice=signed_out")
	if resp.StatusCode != http.StatusSeeOther || resp.Header.Get("Location") != "/login?notice=signed_out" {
		t.Fatalf("status %d, to %q", resp.StatusCode, resp.Header.Get("Location"))
	}
	cookie := cookieIn(resp, "mode")
	if cookie == nil || cookie.Value != "dark" || !cookie.HttpOnly || cookie.SameSite != http.SameSiteLaxMode || cookie.Path != "/" || cookie.Expires.IsZero() {
		t.Fatalf("cookie = %+v", cookie)
	}

	_, page := reading(t, visitor, app.Server.URL+"/login", "en")
	for _, want := range []string{
		`<html lang="en" data-mode="dark">`,
		`<option value="dark" selected>`,
		// The browser's chrome is dark whatever the system asks for.
		`content="#101823" media="(prefers-color-scheme: light)"`,
		darkChrome,
	} {
		if !strings.Contains(page, want) {
			t.Errorf("the page after choosing dark lacks %s:\n%s", want, page)
		}
	}

	chooseMode(t, app, visitor, "light", "/")
	_, page = reading(t, visitor, app.Server.URL+"/trust", "en")
	for _, want := range []string{`data-mode="light"`, lightChrome, `content="#F7F2E8" media="(prefers-color-scheme: dark)"`} {
		if !strings.Contains(page, want) {
			t.Errorf("a document after choosing light lacks %s:\n%s", want, page)
		}
	}

	// Following the system forgets the choice.
	resp = chooseMode(t, app, visitor, "system", "/login")
	if c := cookieIn(resp, "mode"); c == nil || c.MaxAge >= 0 {
		t.Fatalf("following the system did not remove the cookie: %+v", c)
	}
	_, page = reading(t, visitor, app.Server.URL+"/login", "en")
	if strings.Contains(page, "data-mode=") || !strings.Contains(page, lightChrome) || !strings.Contains(page, darkChrome) {
		t.Errorf("the page after following the system again:\n%s", page)
	}
}

// TestMode_OnARespondentsPage: a respondent may choose a display mode
// too, on the survey itself, and is brought back to it.
func TestMode_OnARespondentsPage(t *testing.T) {
	t.Parallel()
	app := apptest.New(t, apptest.Options{})
	creator := app.Login(t, apptest.UniqueEmail("mode"))
	id := app.CreateSurvey(t, creator, "In the dark", true)
	app.AddQuestion(t, creator, id, "long_text", "How was it?", nil)
	app.Publish(t, creator, id)
	share := "/s/" + id + "?lang=en"

	respondent := noRedirects(app.BrowserClient(t))
	_, page := reading(t, respondent, app.Server.URL+share, "en")
	if !strings.Contains(page, `action="/mode"`) || strings.Contains(page, "data-mode=") {
		t.Fatalf("the survey has no display mode switcher, or names a mode unasked:\n%s", page)
	}

	resp := chooseMode(t, app, respondent, "dark", share)
	if resp.Header.Get("Location") != share {
		t.Errorf("the respondent was sent to %q, not back to the survey", resp.Header.Get("Location"))
	}
	_, page = reading(t, respondent, app.Server.URL+share, "en")
	if !strings.Contains(page, `data-mode="dark"`) || !bodyContains(page, "How was it?") {
		t.Errorf("the survey after choosing dark:\n%s", page)
	}
}

// TestMode_WhatIsNotAModeChangesNothing: a value the switcher does not
// offer is not remembered, a cookie holding one is passed over, and a
// place that is not on this site is not gone to.
func TestMode_WhatIsNotAThemeChangesNothing(t *testing.T) {
	t.Parallel()
	app := apptest.New(t, apptest.Options{})
	visitor := noRedirects(app.BrowserClient(t))

	for _, mode := range []string{"", "purple", "Dark", `dark" onload="x`} {
		resp := chooseMode(t, app, visitor, mode, "/login")
		if len(resp.Cookies()) != 0 || resp.Header.Get("Location") != "/login" {
			t.Errorf("%q: cookies %v, to %q", mode, resp.Cookies(), resp.Header.Get("Location"))
		}
	}
	for _, next := range []string{"https://evil.example/", "//evil.example/", "/\\evil.example", "login"} {
		if resp := chooseMode(t, app, visitor, "dark", next); resp.Header.Get("Location") != "/" {
			t.Errorf("next %q sent the visitor to %q", next, resp.Header.Get("Location"))
		}
	}

	stranger := app.BrowserClient(t)
	setRawCookie(t, stranger, app.Server.URL, "mode", "purple")
	_, page := reading(t, stranger, app.Server.URL+"/login", "en")
	if strings.Contains(page, "data-mode=") || !strings.Contains(page, `<option value="system" selected>`) {
		t.Errorf("a cookie holding no display mode was drawn:\n%s", page)
	}
}

// TestMode_AChoiceKeptUnderTheEarlierNameIsStillDrawn: the choice was
// once kept in a cookie named "theme", which a browser holds for a year.
// It is drawn until the reader chooses again, and then removed, so that
// following the system forgets it too.
func TestMode_AChoiceKeptUnderTheEarlierNameIsStillDrawn(t *testing.T) {
	t.Parallel()
	app := apptest.New(t, apptest.Options{})
	visitor := noRedirects(app.BrowserClient(t))
	setRawCookie(t, visitor, app.Server.URL, "theme", "dark")

	_, page := reading(t, visitor, app.Server.URL+"/login", "en")
	if !strings.Contains(page, `data-mode="dark"`) || !strings.Contains(page, `<option value="dark" selected>`) {
		t.Fatalf("a choice kept under the earlier name was not drawn:\n%s", page)
	}

	// A new choice is kept under the new name and wins over the old.
	resp := chooseMode(t, app, visitor, "light", "/login")
	if c := cookieIn(resp, "mode"); c == nil || c.Value != "light" {
		t.Fatalf("the new choice was not kept: %+v", c)
	}
	if c := cookieIn(resp, "theme"); c == nil || c.MaxAge >= 0 {
		t.Fatalf("the earlier cookie was not removed: %+v", c)
	}
	_, page = reading(t, visitor, app.Server.URL+"/login", "en")
	if !strings.Contains(page, `data-mode="light"`) {
		t.Errorf("the page after choosing light over an earlier dark:\n%s", page)
	}

	// Following the system forgets a choice kept under the earlier name.
	stale := noRedirects(app.BrowserClient(t))
	setRawCookie(t, stale, app.Server.URL, "theme", "dark")
	resp = chooseMode(t, app, stale, "system", "/login")
	if c := cookieIn(resp, "theme"); c == nil || c.MaxAge >= 0 {
		t.Fatalf("following the system kept the earlier cookie: %+v", c)
	}
	_, page = reading(t, stale, app.Server.URL+"/login", "en")
	if strings.Contains(page, "data-mode=") {
		t.Errorf("the page after following the system:\n%s", page)
	}
}
