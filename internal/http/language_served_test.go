package http_test

import (
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/TryEarful/earful/internal/apptest"
)

// reading fetches a page as a browser set to a language would, with the
// cookies the client already has.
func reading(t *testing.T, client *http.Client, address, browser string) (*http.Response, string) {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, address, nil)
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	if browser != "" {
		req.Header.Set("Accept-Language", browser)
	}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("GET %s: %v", address, err)
	}
	defer resp.Body.Close()
	return resp, apptest.ReadBody(t, resp)
}

// TestLanguage_FollowsTheBrowser: with nothing chosen, the interface is
// in the language the browser asks for, where it is written in it, and
// in English where it is not or where the browser asks for nothing.
func TestLanguage_FollowsTheBrowser(t *testing.T) {
	t.Parallel()
	app := apptest.New(t, apptest.Options{})
	anyone := &http.Client{}

	for browser, want := range map[string][2]string{
		"":                                 {`<html lang="en">`, "Email me a link to sign in"},
		"en-GB,en;q=0.9":                   {`<html lang="en">`, "Email me a link to sign in"},
		"es":                               {`<html lang="es">`, "Enviarme un enlace de acceso"},
		"es-AR,es;q=0.9,en;q=0.8":          {`<html lang="es">`, "Enviarme un enlace de acceso"},
		"nl-BE,nl;q=0.9,es;q=0.8,en;q=0.7": {`<html lang="es">`, "Enviarme un enlace de acceso"},
		"nl,de;q=0.9":                      {`<html lang="en">`, "Email me a link to sign in"},
		"not a language at all":            {`<html lang="en">`, "Email me a link to sign in"},
	} {
		_, page := reading(t, anyone, app.Server.URL+"/login", browser)
		if !strings.Contains(page, want[0]) || !bodyContains(page, want[1]) {
			t.Errorf("a browser asking for %q was answered:\n%s", browser, page)
		}
	}
}

// TestLanguage_TheSwitcherIsRemembered: a choice made with the switcher
// outranks the browser, on the page it was made on and the ones after,
// and it is made without a session, because the sign-in page has a
// switcher too.
func TestLanguage_TheSwitcherIsRemembered(t *testing.T) {
	t.Parallel()
	app := apptest.New(t, apptest.Options{})
	visitor := app.BrowserClient(t)
	visitor.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }

	_, page := reading(t, visitor, app.Server.URL+"/login", "en")
	for _, want := range []string{`action="/language"`, `name="next" value="/login"`, `lang="es"`, "Español", "English"} {
		if !strings.Contains(page, want) {
			t.Fatalf("the switcher is not on the sign-in page (%s):\n%s", want, page)
		}
	}

	resp, err := visitor.PostForm(app.Server.URL+"/language", url.Values{"lang": {"es"}, "next": {"/login"}})
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusSeeOther || resp.Header.Get("Location") != "/login" {
		t.Fatalf("status %d, to %q", resp.StatusCode, resp.Header.Get("Location"))
	}
	var cookie *http.Cookie
	for _, c := range resp.Cookies() {
		if c.Name == "interface_lang" {
			cookie = c
		}
	}
	if cookie == nil || cookie.Value != "es" || !cookie.HttpOnly || cookie.SameSite != http.SameSiteLaxMode || cookie.Path != "/" {
		t.Fatalf("cookie = %+v", cookie)
	}

	// The browser still asks for English, and is answered in Spanish.
	_, page = reading(t, visitor, app.Server.URL+"/login", "en")
	if !bodyContains(page, "Iniciar sesión") || !strings.Contains(page, `<html lang="es">`) {
		t.Errorf("the choice was not kept:\n%s", page)
	}

	// A language the interface is not written in is not remembered, and
	// a place that is not on this site is not gone to.
	stranger := app.BrowserClient(t)
	stranger.CheckRedirect = visitor.CheckRedirect
	resp, err = stranger.PostForm(app.Server.URL+"/language", url.Values{"lang": {"nl"}, "next": {"https://evil.example/"}})
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	resp.Body.Close()
	if len(resp.Cookies()) != 0 || resp.Header.Get("Location") != "/" {
		t.Errorf("cookies %v, to %q", resp.Cookies(), resp.Header.Get("Location"))
	}
}

// TestLanguage_ARespondentsIsReadAndNotKept is story 25 for the
// interface: a respondent's page is worded for them from their request,
// and nothing about what they read is kept or looked up.
func TestLanguage_ARespondentsIsReadAndNotKept(t *testing.T) {
	t.Parallel()
	app := apptest.New(t, apptest.Options{AI: translatorFake()})
	creator := app.Login(t, apptest.UniqueEmail("respondent-lang"))
	id := dutchChoiceSurvey(t, app, creator, "Whose language", true)
	share := app.Server.URL + "/s/" + id

	// The browser's language, where the survey's was not chosen.
	resp, page := reading(t, &http.Client{}, share, "es-MX,es;q=0.9")
	if !bodyContains(page, "Enviar respuestas") || !bodyContains(page, "Pregunta 1 de 2") {
		t.Errorf("a Spanish browser was not answered in Spanish:\n%s", page)
	}
	if !bodyContains(page, "How often?") {
		t.Errorf("the questions are not as the creator wrote them:\n%s", page)
	}
	for _, c := range resp.Cookies() {
		if strings.Contains(strings.ToLower(c.Name), "lang") {
			t.Errorf("a respondent's language was kept in a cookie: %s", c.Name)
		}
	}

	// The survey's language, which outranks the browser's for the
	// questions and is not one the interface is written in: the page
	// declares the language of its questions, and is worded in the
	// browser's.
	_, page = reading(t, &http.Client{}, share+"?lang=nl", "es")
	if !strings.Contains(page, `<html lang="nl">`) || !bodyContains(page, "Hoe vaak?") || !bodyContains(page, "Enviar respuestas") {
		t.Errorf("a Dutch survey in a Spanish browser:\n%s", page)
	}

	// The switcher's cookie is somebody's remembered choice, and a
	// respondent has none: it is not read here.
	chooser := app.BrowserClient(t)
	chooser.PostForm(app.Server.URL+"/language", url.Values{"lang": {"es"}, "next": {"/"}})
	if _, page := reading(t, chooser, app.Server.URL+"/login", "en"); !bodyContains(page, "Iniciar sesión") {
		t.Fatalf("the cookie was not set:\n%s", page)
	}
	_, page = reading(t, chooser, share, "en")
	if !bodyContains(page, "Submit answers") || strings.Contains(page, `action="/language"`) {
		t.Errorf("a respondent's page read the switcher's cookie, or offered the switcher:\n%s", page)
	}
}

// TestLanguage_ACreatorsPageDoesNotReadLang: on the results page ?lang=
// is the language answers are translated into. It says nothing about
// the language of the page.
func TestLanguage_ACreatorsPageDoesNotReadLang(t *testing.T) {
	t.Parallel()
	app := apptest.New(t, apptest.Options{})
	creator := app.Login(t, apptest.UniqueEmail("creator-lang"))
	id := app.CreateSurvey(t, creator, "Read in English", true)
	app.AddQuestion(t, creator, id, "long_text", "How was it?", nil)
	app.Publish(t, creator, id)

	_, page := reading(t, creator, app.Server.URL+"/surveys/"+id+"/results?lang=es", "en")
	if !bodyContains(page, "No responses yet") || !strings.Contains(page, `<html lang="en">`) {
		t.Errorf("the page took the language of the translation for its own:\n%s", page)
	}
	_, page = reading(t, creator, app.Server.URL+"/surveys/"+id+"/results", "es")
	if !bodyContains(page, "Todavía no hay envíos") || !bodyContains(page, "Abierta") {
		t.Errorf("a creator with a Spanish browser:\n%s", page)
	}
}

// TestLanguage_WhatIsWordedOutsideATemplate: an error from the domain,
// a date, and an email, each of which is worded somewhere that is not a
// template and each for the person it is for.
func TestLanguage_WhatIsWordedOutsideATemplate(t *testing.T) {
	t.Parallel()
	app := apptest.New(t, apptest.Options{})
	creator := app.BrowserClient(t)
	creator.PostForm(app.Server.URL+"/language", url.Values{"lang": {"es"}, "next": {"/"}})
	addr := apptest.UniqueEmail("hispanohablante")

	// The email is asked for in Spanish, and written in it.
	resp, err := creator.PostForm(app.Server.URL+"/auth/magic/request", url.Values{"email": {addr}})
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	page := apptest.ReadBody(t, resp)
	resp.Body.Close()
	if !bodyContains(page, "Revise su correo") {
		t.Errorf("the page after asking:\n%s", page)
	}
	sent := app.Emails.To(addr)
	if len(sent) != 1 || sent[0].Subject != "Su enlace de acceso a Earful" ||
		!strings.Contains(sent[0].Text, "Pulse para iniciar sesión en Earful:") ||
		!strings.Contains(sent[0].Text, "/auth/magic/verify?token=") {
		t.Fatalf("emails = %+v", sent)
	}

	app.LoginWithClient(t, creator, addr)

	// The workspace was named when the account was made, in Spanish.
	_, page = reading(t, creator, app.Server.URL+"/account", "")
	if !bodyContains(page, "Espacio de trabajo de hispanohablante") {
		t.Errorf("the workspace's name:\n%s", page)
	}

	// An error made in the domain, which knows no Spanish.
	resp = app.PostForm(t, creator, "/surveys", url.Values{"title": {"   "}, "anonymity": {"anonymous"}})
	page = apptest.ReadBody(t, resp)
	resp.Body.Close()
	if !bodyContains(page, "Póngale un título a la encuesta") {
		t.Errorf("a survey with no title:\n%s", page)
	}

	// A date, which Go would write in English.
	id := app.CreateSurvey(t, creator, "Una encuesta", true)
	page = app.SurveyPage(t, creator, id)
	year := app.Clock.Now().Format("2006")
	if !strings.Contains(page, " de "+year) || !bodyContains(page, "Encuesta anónima") || !bodyContains(page, "Borrador") {
		t.Errorf("the editor:\n%s", page)
	}
}

// TestLanguage_DocumentsAreReadInTheirReadersLanguage: a document is
// served in translation where it has one, says that it is one, and can
// be read in the original from the address.
func TestLanguage_DocumentsAreReadInTheirReadersLanguage(t *testing.T) {
	t.Parallel()
	app := apptest.New(t, apptest.Options{})
	anyone := &http.Client{}

	_, page := reading(t, anyone, app.Server.URL+"/trust", "es")
	for _, want := range []string{"Su voz nunca se guarda", "Esta página es una traducción", `href="/trust?lang=en"`, "Última actualización", `<html lang="es">`} {
		if !bodyContains(page, want) && !strings.Contains(page, want) {
			t.Errorf("the trust page in Spanish lacks %q:\n%s", want, page)
		}
	}
	_, page = reading(t, anyone, app.Server.URL+"/trust?lang=en", "es")
	if !bodyContains(page, "Your voice is never stored") || !bodyContains(page, "Last updated") {
		t.Errorf("the original, asked for by its address:\n%s", page)
	}
	resp, markdown := reading(t, anyone, app.Server.URL+"/help/voice.md", "es")
	if resp.Header.Get("Content-Language") != "es" || !strings.Contains(markdown, "# Responder con la voz") {
		t.Errorf("the Markdown: %q\n%s", resp.Header.Get("Content-Language"), markdown)
	}
}
