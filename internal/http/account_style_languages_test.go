package http_test

import (
	"context"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/TryEarful/earful/internal/ai"
	"github.com/TryEarful/earful/internal/apptest"
)

// The account style's words in other languages (ADR-0023): translated
// once, on the account, for every survey that follows its style, and
// reviewed as a survey's Languages tab reviews them.

// worded gives an account a style with words to translate: a tagline, a
// header link and a footer text.
func worded(t *testing.T, app *apptest.App, creator *http.Client, tagline string) {
	t.Helper()
	resp, page := postAccountStyle(t, app, creator, url.Values{
		"header_name":       {"Corner Workshop"},
		"header_tagline":    {tagline},
		"header_link_label": {"Courses", ""},
		"header_link_url":   {"https://example.com/courses", ""},
		"footer_text":       {"Corner Workshop Ltd"},
	})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("saving the account's style: status %d\n%s", resp.StatusCode, page)
	}
}

// translatedSurvey makes a survey of the creator's being translated into
// lang, which is what puts lang on the account style's translations page.
func translatedSurvey(t *testing.T, app *apptest.App, creator *http.Client, lang string) string {
	t.Helper()
	id := app.CreateSurvey(t, creator, "In "+lang, true)
	resp := app.PostForm(t, creator, "/surveys/"+id+"/localizations", url.Values{"lang": {lang}})
	resp.Body.Close()
	return id
}

func postAccountTranslation(t *testing.T, app *apptest.App, creator *http.Client, path string, form url.Values) (*http.Response, string) {
	t.Helper()
	resp := app.PostForm(t, creator, path, form)
	defer resp.Body.Close()
	return resp, apptest.ReadBody(t, resp)
}

// languagePanel is the part of the page about one language.
func languagePanel(t *testing.T, page, lang string) string {
	t.Helper()
	start := strings.Index(page, `id="lang-`+lang+`"`)
	if start < 0 {
		t.Fatalf("no panel for %s on the page:\n%s", lang, page)
	}
	rest := page[start:]
	if end := strings.Index(rest, "</section>"); end >= 0 {
		rest = rest[:end]
	}
	return rest
}

// TestAccountStyleLanguages_DraftedThenReviewed: the languages offered are
// the ones the workspace's surveys are translated into; a drafted
// translation is stored unreviewed and counted against the workspace for
// no survey; saving marks it reviewed; and a change to the account's
// wording sends it back for reading.
func TestAccountStyleLanguages_DraftedThenReviewed(t *testing.T) {
	t.Parallel()
	fake := &ai.Fake{TranslateScript: [][]string{{"traducido"}}}
	app := apptest.New(t, apptest.Options{AI: fake})
	address := apptest.UniqueEmail("account-langs")
	creator := app.Login(t, address)
	worded(t, app, creator, "Evening classes in wood and clay.")
	translatedSurvey(t, app, creator, "es")

	page := mustGet(t, creator, app.Server.URL+"/account/style")
	if !bodyContains(page, `href="/account/style/languages"`) {
		t.Fatalf("the account's style page does not lead to its translations:\n%s", page)
	}
	page = mustGet(t, creator, app.Server.URL+"/account/style/languages")
	panel := languagePanel(t, page, "es")
	for _, want := range []string{"To review", "Evening classes in wood and clay.", "Courses", "Corner Workshop Ltd", "js-account-style-draft"} {
		if !strings.Contains(panel, want) {
			t.Errorf("the Spanish panel does not show %q:\n%s", want, panel)
		}
	}
	if strings.Contains(panel, "js-account-style-remove") {
		t.Error("a language a survey is translated into should not be removable")
	}
	// The name and the addresses are the same in every language.
	if strings.Contains(panel, "https://example.com/courses") {
		t.Error("a link's address is offered for translating")
	}

	resp, page := postAccountTranslation(t, app, creator, "/account/style/languages/es/draft", nil)
	if resp.StatusCode != http.StatusOK || !bodyContains(page, "Drafted the translation in Spanish") {
		t.Fatalf("drafting: status %d\n%s", resp.StatusCode, page)
	}
	if len(fake.TranslateCalls) != 3 {
		t.Errorf("drafting asked for %d translations, want 3 (tagline, link, footer)", len(fake.TranslateCalls))
	}
	panel = languagePanel(t, page, "es")
	if !strings.Contains(panel, "To review") || !strings.Contains(panel, "Drafted by AI, not yet reviewed.") || !strings.Contains(panel, "traducido") {
		t.Errorf("a drafted translation should be shown unreviewed:\n%s", panel)
	}
	if usage := accountTranslationUsage(t, app, address); usage == 0 {
		t.Error("drafting was not counted against the workspace as a translation for no survey")
	}

	resp, page = postAccountTranslation(t, app, creator, "/account/style/languages/es", url.Values{
		"style_tagline":     {"Clases de tarde en madera y barro."},
		"style_header_link": {"Cursos"},
		"style_footer_text": {"Corner Workshop SL"},
	})
	if resp.StatusCode != http.StatusOK || !bodyContains(page, "Saved and marked reviewed in Spanish.") {
		t.Fatalf("saving: status %d\n%s", resp.StatusCode, page)
	}
	// A reviewed translation asks first about the open surveys it reaches,
	// the Starter Survey among them; the language is on its page.
	page = mustGet(t, creator, app.Server.URL+"/account/style/languages")
	panel = languagePanel(t, page, "es")
	if !strings.Contains(panel, "Reviewed") || !strings.Contains(panel, "Clases de tarde en madera y barro.") {
		t.Errorf("a saved translation should be reviewed:\n%s", panel)
	}

	// A drafted translation does not overwrite one already reviewed.
	if _, page = postAccountTranslation(t, app, creator, "/account/style/languages/es/draft", nil); !bodyContains(page, "Nothing left to translate in Spanish.") {
		t.Errorf("drafting a reviewed language should leave it alone:\n%s", page)
	}

	worded(t, app, creator, "Weekend classes in wood and clay.")
	panel = languagePanel(t, mustGet(t, creator, app.Server.URL+"/account/style/languages"), "es")
	if !strings.Contains(panel, "Your account style changed after this was translated.") || !strings.Contains(panel, "To review") {
		t.Errorf("a translation of an earlier wording should be read again:\n%s", panel)
	}
}

// accountTranslationUsage is how much translation the workspace of the
// creator signed in as address has been counted for no survey, which
// only the account's style is.
func accountTranslationUsage(t *testing.T, app *apptest.App, address string) int64 {
	t.Helper()
	pool, err := pgxpool.New(context.Background(), app.DSN)
	if err != nil {
		t.Fatalf("pool: %v", err)
	}
	defer pool.Close()
	var tokens int64
	if err := pool.QueryRow(context.Background(), `
		SELECT coalesce(sum(a.tokens), 0)
		FROM ai_usage a
		JOIN workspace_members m ON m.workspace_id = a.workspace_id
		JOIN users u ON u.id = m.user_id
		WHERE u.email = $1 AND a.survey_id IS NULL AND a.kind = 'translate'`, address).Scan(&tokens); err != nil {
		t.Fatalf("ai usage: %v", err)
	}
	return tokens
}

// TestAccountStyleLanguages_ByHand: without a text model there is no
// drafting, and the words are written by hand; a language no survey is
// translated into any longer can be removed, one a survey carries cannot,
// and one nobody uses cannot be started by address.
func TestAccountStyleLanguages_ByHand(t *testing.T) {
	t.Parallel()
	app := apptest.New(t, apptest.Options{})
	creator := app.Login(t, apptest.UniqueEmail("account-langs-hand"))
	worded(t, app, creator, "Evening classes.")
	survey := translatedSurvey(t, app, creator, "fr")

	page := mustGet(t, creator, app.Server.URL+"/account/style/languages")
	if bodyContains(page, "js-account-style-draft") {
		t.Error("without a text model there should be no drafting")
	}
	if resp, page := postAccountTranslation(t, app, creator, "/account/style/languages/fr", url.Values{
		"style_tagline": {"Cours du soir."}, "style_header_link": {"Cours"}, "style_footer_text": {"Corner Workshop SARL"},
	}); resp.StatusCode != http.StatusOK || !strings.Contains(languagePanel(t, page, "fr"), "Reviewed") {
		t.Fatalf("saving by hand: status %d\n%s", resp.StatusCode, page)
	}

	if resp, page := postAccountTranslation(t, app, creator, "/account/style/languages/fr/remove", nil); resp.StatusCode != http.StatusUnprocessableEntity || !bodyContains(page, "so its translation stays") {
		t.Errorf("removing a language a survey carries: status %d\n%s", resp.StatusCode, page)
	}
	if resp, page := postAccountTranslation(t, app, creator, "/account/style/languages/nl", url.Values{"style_tagline": {"Avondlessen."}}); resp.StatusCode != http.StatusNotFound || !bodyContains(page, "None of your surveys is translated into that language.") {
		t.Errorf("a language no survey carries was started by address: status %d\n%s", resp.StatusCode, page)
	}
	if resp, _ := postAccountTranslation(t, app, creator, "/account/style/languages/fr", url.Values{"style_tagline": {"   "}}); resp.StatusCode != http.StatusUnprocessableEntity {
		t.Errorf("saving no words: status %d, want 422", resp.StatusCode)
	}

	// Once the survey stops carrying French, its translation can go.
	app.PostForm(t, creator, "/surveys/"+survey+"/localizations/fr/delete", nil).Body.Close()
	page = mustGet(t, creator, app.Server.URL+"/account/style/languages")
	if !strings.Contains(languagePanel(t, page, "fr"), "js-account-style-remove") {
		t.Fatalf("a translation no survey needs should be removable:\n%s", page)
	}
	if resp, page := postAccountTranslation(t, app, creator, "/account/style/languages/fr/remove", nil); resp.StatusCode != http.StatusOK || bodyContains(page, `id="lang-fr"`) {
		t.Errorf("removing: status %d\n%s", resp.StatusCode, page)
	}
}

// TestAccountStyleLanguages_Guarded: a form without its token is refused,
// another workspace's translations are untouched, an account with no
// words says so, and a workspace over its AI allowance is told as the
// Languages tab tells it.
func TestAccountStyleLanguages_Guarded(t *testing.T) {
	t.Parallel()
	app := apptest.New(t, apptest.Options{AI: &ai.Fake{TranslateScript: [][]string{{"traducido"}}}, AIQuota: 1})
	creator := app.Login(t, apptest.UniqueEmail("account-langs-guard"))

	page := mustGet(t, creator, app.Server.URL+"/account/style/languages")
	if !bodyContains(page, "js-account-style-languages-empty") || !bodyContains(page, "no words to translate yet") {
		t.Errorf("an account style with no words should say there is nothing to translate:\n%s", page)
	}

	worded(t, app, creator, "Evening classes.")
	translatedSurvey(t, app, creator, "es")
	if resp, _ := postAccountTranslation(t, app, creator, "/account/style/languages/es", url.Values{"_csrf": {"forged"}, "style_tagline": {"Clases."}}); resp.StatusCode != http.StatusForbidden {
		t.Errorf("a forged form: status %d, want 403", resp.StatusCode)
	}

	stranger := app.Login(t, apptest.UniqueEmail("account-langs-stranger"))
	if resp, _ := postAccountTranslation(t, app, stranger, "/account/style/languages/es", url.Values{"style_tagline": {"Hackeado."}}); resp.StatusCode == http.StatusSeeOther || resp.StatusCode == http.StatusOK {
		t.Errorf("another workspace saved a translation: status %d", resp.StatusCode)
	}
	if panel := languagePanel(t, mustGet(t, creator, app.Server.URL+"/account/style/languages"), "es"); strings.Contains(panel, "Hackeado.") {
		t.Error("another workspace's save reached this account's translations")
	}

	// The first drafting spends the allowance of 1; the next is refused.
	postAccountTranslation(t, app, creator, "/account/style/languages/es/draft", nil)
	worded(t, app, creator, "Weekend classes.")
	if _, page := postAccountTranslation(t, app, creator, "/account/style/languages/es/draft", nil); !bodyContains(page, "AI allowance") {
		t.Errorf("a workspace over its allowance should be told so:\n%s", page)
	}
}

var accountSourceRe = regexp.MustCompile(`<input type="hidden" name="source" value="([0-9a-f]+)"`)

// TestAccountStyleLanguages_AWordingChangedSinceIsReadAgain: a page
// drawn before the account's wording changed does not mark a
// translation of the earlier wording as read, nor ask the model to draft
// one; it is shown again with the wording as it now stands.
func TestAccountStyleLanguages_AWordingChangedSinceIsReadAgain(t *testing.T) {
	t.Parallel()
	fake := &ai.Fake{TranslateScript: [][]string{{"traducido"}}}
	app := apptest.New(t, apptest.Options{AI: fake})
	creator := app.Login(t, apptest.UniqueEmail("account-langs-changed"))
	worded(t, app, creator, "Evening classes.")
	translatedSurvey(t, app, creator, "es")

	page := mustGet(t, creator, app.Server.URL+"/account/style/languages")
	m := accountSourceRe.FindStringSubmatch(page)
	if m == nil {
		t.Fatalf("the translations page does not say which wording it shows:\n%s", page)
	}
	drawnFrom := m[1]

	// The tagline changes in another tab.
	app.Clock.Advance(time.Second)
	worded(t, app, creator, "Weekend classes.")

	resp, body := postAccountTranslation(t, app, creator, "/account/style/languages/es", url.Values{
		"source":            {drawnFrom},
		"style_tagline":     {"Clases de tarde."},
		"style_header_link": {"Cursos"},
		"style_footer_text": {"Corner Workshop SL"},
	})
	if resp.StatusCode != http.StatusConflict || !bodyContains(body, "changed while this page was open") {
		t.Fatalf("saving a translation of an earlier wording: status %d\n%s", resp.StatusCode, body)
	}
	if bodyContains(languagePanel(t, body, "es"), "Clases de tarde.") {
		t.Errorf("a translation of the earlier wording was stored")
	}
	if !bodyContains(body, "Weekend classes.") {
		t.Errorf("the page is not shown again with the wording as it stands:\n%s", body)
	}

	resp, body = postAccountTranslation(t, app, creator, "/account/style/languages/es/draft", url.Values{"source": {drawnFrom}})
	if resp.StatusCode != http.StatusConflict || !bodyContains(body, "changed while this page was open") {
		t.Fatalf("drafting from an earlier wording: status %d\n%s", resp.StatusCode, body)
	}
	if len(fake.TranslateCalls) != 0 {
		t.Errorf("the model was asked to draft from a page drawn before the wording changed")
	}

	// From the page as it now stands, the save goes through.
	current := accountSourceRe.FindStringSubmatch(body)[1]
	resp, body = postAccountTranslation(t, app, creator, "/account/style/languages/es", url.Values{
		"source":            {current},
		"style_tagline":     {"Clases de fin de semana."},
		"style_header_link": {"Cursos"},
		"style_footer_text": {"Corner Workshop SL"},
	})
	if resp.StatusCode != http.StatusOK || bodyContains(body, "changed while this page was open") {
		t.Errorf("saving from the current page: status %d\n%s", resp.StatusCode, body)
	}
}

// changingTranslator is a fake model during whose answer the account's
// wording changes, as it would if the creator saved it in another tab
// while a draft was being written.
type changingTranslator struct {
	*ai.Fake
	during func()
	once   sync.Once
}

func (c *changingTranslator) Translate(ctx context.Context, req ai.TranslateRequest) (ai.Stream, error) {
	c.once.Do(c.during)
	return c.Fake.Translate(ctx, req)
}

// TestAccountStyleLanguages_ADraftOfWordingChangedMeanwhileIsNotStored: a
// draft is of the words the request read, so if the account's wording
// changes while the model writes it, the draft is refused, even from a
// form that says nothing about the wording it was drawn from.
func TestAccountStyleLanguages_ADraftOfWordingChangedMeanwhileIsNotStored(t *testing.T) {
	t.Parallel()
	model := &changingTranslator{Fake: &ai.Fake{TranslateScript: [][]string{{"traducido"}}}}
	app := apptest.New(t, apptest.Options{AI: model})
	creator := app.Login(t, apptest.UniqueEmail("account-langs-meanwhile"))
	worded(t, app, creator, "Evening classes.")
	translatedSurvey(t, app, creator, "es")
	model.during = func() {
		app.Clock.Advance(time.Second)
		postAccountStyle(t, app, creator, url.Values{
			"header_name":       {"Corner Workshop"},
			"header_tagline":    {"Weekend classes."},
			"header_link_label": {"Courses", ""},
			"header_link_url":   {"https://example.com/courses", ""},
			"footer_text":       {"Corner Workshop Ltd"},
		})
	}

	resp, body := postAccountTranslation(t, app, creator, "/account/style/languages/es/draft", url.Values{})
	if resp.StatusCode != http.StatusConflict || !bodyContains(body, "changed while this page was open") {
		t.Fatalf("a draft of wording changed meanwhile: status %d\n%s", resp.StatusCode, body)
	}
	if len(model.TranslateCalls) == 0 {
		t.Fatalf("the model was never asked, so nothing tested the save")
	}
	if bodyContains(languagePanel(t, body, "es"), "traducido") {
		t.Errorf("a draft of the earlier wording was stored")
	}
}
