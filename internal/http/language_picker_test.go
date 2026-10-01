package http_test

import (
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/TryEarful/earful/internal/apptest"
)

// The respondent's language picker offers every language the interface
// is written in as well as every language the survey was translated
// into (story 25). Choosing one the survey has no translation into words
// the page in it, keeps the questions as written, and says so.

const untranslatedNoticeES = "Las preguntas se muestran tal como se escribieron, sin traducir"

// englishOnlySurvey publishes a survey with one required question and no
// translation, and returns its id.
func englishOnlySurvey(t *testing.T, app *apptest.App, creator *http.Client, title string, anonymous bool) string {
	t.Helper()
	id := app.CreateSurvey(t, creator, title, anonymous)
	app.AddQuestion(t, creator, id, "short_text", "What brought you here?", url.Values{"required": {"on"}})
	if body := app.Publish(t, creator, id); !bodyContains(body, "Published version 1") {
		t.Fatalf("publish refused:\n%s", body)
	}
	return id
}

// TestLanguagePicker_OffersTheInterfaceLanguages: a survey with no
// translation still offers every language the page can be worded in, by
// the name each gives itself, and says nothing about a translation when
// none was chosen.
func TestLanguagePicker_OffersTheInterfaceLanguages(t *testing.T) {
	t.Parallel()
	app := apptest.New(t, apptest.Options{})
	creator := app.Login(t, apptest.UniqueEmail("picker-offers"))
	id := englishOnlySurvey(t, app, creator, "Picker offers", true)

	_, page := reading(t, &http.Client{}, app.Server.URL+"/s/"+id, "en")
	for _, want := range []string{`class="js-language-picker"`, `value="en" lang="en"`, `value="es" lang="es"`, "Español"} {
		if !strings.Contains(page, want) {
			t.Errorf("the picker lacks %q:\n%s", want, page)
		}
	}
	if strings.Contains(page, "js-untranslated-notice") {
		t.Errorf("a notice with nothing chosen:\n%s", page)
	}
}

// TestLanguagePicker_AnUntranslatedChoiceWordsThePage: Spanish chosen on
// a survey written only in English gives Spanish buttons, the English
// questions, a notice in Spanish, and keeps the choice on every address
// the page leads to, through to the thanks page.
func TestLanguagePicker_AnUntranslatedChoiceWordsThePage(t *testing.T) {
	t.Parallel()
	app := apptest.New(t, apptest.Options{})
	creator := app.Login(t, apptest.UniqueEmail("picker-untranslated"))
	id := englishOnlySurvey(t, app, creator, "Picker untranslated", true)

	respondent := &http.Client{}
	_, page := reading(t, respondent, app.Server.URL+"/s/"+id+"?lang=es", "en")
	for _, want := range []string{"Enviar respuestas", "What brought you here?", untranslatedNoticeES} {
		if !bodyContains(page, want) {
			t.Errorf("the page lacks %q:\n%s", want, page)
		}
	}
	for _, want := range []string{`action="/s/` + id + `?lang=es"`, `href="/help?lang=es"`, `value="es" lang="es" selected`} {
		if !strings.Contains(page, want) {
			t.Errorf("the choice is not kept: no %q:\n%s", want, page)
		}
	}

	// A correction comes back in the language chosen, notice and all.
	resp, again := submitAfterReadingTo(t, app, respondent, "/s/"+id+"?lang=es", respondForm(t, page))
	if resp.StatusCode != http.StatusUnprocessableEntity {
		t.Fatalf("an empty required answer was accepted: %d", resp.StatusCode)
	}
	if !bodyContains(again, untranslatedNoticeES) || !strings.Contains(again, `action="/s/`+id+`?lang=es"`) {
		t.Errorf("the correction lost the language:\n%s", again)
	}

	form := respondForm(t, again)
	form.Set("q_"+extractAnswerFields(t, again)[0], "Una recomendación")
	_, thanks := submitAfterReadingTo(t, app, respondent, "/s/"+id+"?lang=es", form)
	if !bodyContains(thanks, "Gracias") || !strings.Contains(thanks, `href="/help?lang=es"`) {
		t.Errorf("the thanks page is not in the language chosen:\n%s", thanks)
	}
}

// TestLanguagePicker_ATranslatedChoiceNeedsNoNotice: where the survey has
// the language chosen, its questions are in it and nothing more is said;
// where it has a language the interface is not written in, the questions
// are in that one and the page is worded as it would be anyway.
func TestLanguagePicker_ATranslatedChoiceNeedsNoNotice(t *testing.T) {
	t.Parallel()
	app := apptest.New(t, apptest.Options{AI: translatorFake()})
	creator := app.Login(t, apptest.UniqueEmail("picker-translated"))
	id := app.CreateSurvey(t, creator, "Picker translated", true)
	app.AddQuestion(t, creator, id, "short_text", "How often?", nil)
	identity := app.QuestionIdentities(t, creator, id)[0]
	for lang, text := range map[string]string{"es": "¿Con qué frecuencia?", "nl": "Hoe vaak?"} {
		app.PostForm(t, creator, "/surveys/"+id+"/localizations", url.Values{"lang": {lang}}).Body.Close()
		app.PostForm(t, creator, "/surveys/"+id+"/localizations/"+lang, url.Values{"t_" + identity: {text}}).Body.Close()
	}
	if body := app.Publish(t, creator, id); !bodyContains(body, "Published version 1") {
		t.Fatalf("publish refused:\n%s", body)
	}
	share := app.Server.URL + "/s/" + id

	_, page := reading(t, &http.Client{}, share+"?lang=es", "en")
	if !bodyContains(page, "¿Con qué frecuencia?") || !bodyContains(page, "Enviar respuestas") {
		t.Errorf("the Spanish translation was not served in Spanish:\n%s", page)
	}
	if strings.Contains(page, "js-untranslated-notice") {
		t.Errorf("a notice on a translated survey:\n%s", page)
	}

	_, page = reading(t, &http.Client{}, share+"?lang=nl", "en")
	if !bodyContains(page, "Hoe vaak?") || !bodyContains(page, "Submit answers") {
		t.Errorf("the Dutch translation was not served with the usual wording:\n%s", page)
	}
	if strings.Contains(page, "js-untranslated-notice") {
		t.Errorf("a notice on a translated survey:\n%s", page)
	}
	// Each language is offered once, whether the interface, the survey
	// or both are in it.
	if n := strings.Count(page, `value="es"`); n != 1 {
		t.Errorf("Spanish is offered %d times:\n%s", n, page)
	}
	if !strings.Contains(page, `value="nl" selected`) {
		t.Errorf("the Dutch choice is not shown as chosen:\n%s", page)
	}
}

// TestLanguagePicker_PersonalLinksToo: a participant gets the same
// choice, and the page that says they have answered stays in it.
func TestLanguagePicker_PersonalLinksToo(t *testing.T) {
	t.Parallel()
	app := apptest.New(t, apptest.Options{})
	creator := app.Login(t, apptest.UniqueEmail("picker-invite"))
	id := englishOnlySurvey(t, app, creator, "Picker invitation", false)

	addr := apptest.UniqueEmail("participante")
	app.PostForm(t, creator, "/surveys/"+id+"/participants", url.Values{"emails": {addr}}).Body.Close()
	sendInvites(t, app, creator, id)
	link := inviteLinkTo(t, app, addr)
	token := link[strings.LastIndex(link, "/")+1:]

	participant := &http.Client{}
	_, page := reading(t, participant, link+"?lang=es", "en")
	for _, want := range []string{"Enviar respuestas", "What brought you here?", untranslatedNoticeES} {
		if !bodyContains(page, want) {
			t.Errorf("the personal page lacks %q:\n%s", want, page)
		}
	}
	if !strings.Contains(page, `action="/p/`+token+`?lang=es"`) || !strings.Contains(page, `value="es" lang="es" selected`) {
		t.Errorf("the personal page does not keep the choice:\n%s", page)
	}

	form := respondForm(t, page)
	form.Set("q_"+extractAnswerFields(t, page)[0], "Una invitación")
	_, thanks := submitAfterReadingTo(t, app, participant, "/p/"+token+"?lang=es", form)
	if !bodyContains(thanks, "Gracias") {
		t.Fatalf("the participant was not thanked in Spanish:\n%s", thanks)
	}
	_, again := reading(t, participant, link+"?lang=es", "en")
	if !bodyContains(again, "Ya ha respondido") {
		t.Errorf("the answered page is not in the language chosen:\n%s", again)
	}
}
