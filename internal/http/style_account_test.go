package http_test

import (
	"html"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"testing"

	"github.com/TryEarful/earful/internal/apptest"
)

// A survey follows its account's style (ADR-0023) until it makes a part
// its own, takes a change to the account's style when it is next
// published, and can turn its header and footer off.

// accountWorkshop is the account style these tests set: Ocean, a header
// and a footer, the words of the workshop style.
func accountWorkshop(t *testing.T, app *apptest.App, creator *http.Client) {
	t.Helper()
	if resp, page := postAccountStyle(t, app, creator, workshopStyle()); resp.StatusCode != http.StatusOK {
		t.Fatalf("saving the account's style: status %d\n%s", resp.StatusCode, page)
	}
}

// styleTabForm is the Style tab's form as the page posts it, with both
// boxes offered and ticked.
func styleTabForm(form url.Values) url.Values {
	form.Set("header_offered", "1")
	form.Set("custom_header", "1")
	form.Set("footer_offered", "1")
	form.Set("custom_footer", "1")
	if !form.Has("logo_alt") {
		form.Set("logo_alt", "")
	}
	return form
}

func postStyleTab(t *testing.T, app *apptest.App, creator *http.Client, id string, form url.Values) string {
	t.Helper()
	resp := app.PostForm(t, creator, "/surveys/"+id+"/style", form)
	defer resp.Body.Close()
	page := apptest.ReadBody(t, resp)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("saving the Style tab: status %d\n%s", resp.StatusCode, page)
	}
	return page
}

var sourceChipRe = regexp.MustCompile(`<span class="chip js-style-source">([^<]*)</span>`)

// partSources are the chips on the Style tab, in the order of its
// sections: theme, header, footer, thanks picture.
func partSources(page string) []string {
	var out []string
	for _, m := range sourceChipRe.FindAllStringSubmatch(page, -1) {
		out = append(out, html.UnescapeString(m[1]))
	}
	return out
}

func preview(t *testing.T, app *apptest.App, creator *http.Client, id string) string {
	t.Helper()
	return mustGet(t, creator, app.Server.URL+"/surveys/"+id+"/preview")
}

// TestStyleAccount_APublishedVersionKeepsItsLook: changing the account's
// style changes no published survey. Its editor says so, its preview
// shows the account's style, and publishing applies it, pictures and
// all. A response begun on the earlier version is thanked in its look.
func TestStyleAccount_APublishedVersionKeepsItsLook(t *testing.T) {
	t.Parallel()
	app := apptest.New(t, apptest.Options{})
	creator := app.Login(t, apptest.UniqueEmail("style-account-live"))
	id := publishedSurvey(t, app, creator, "Before the account style", true, [3]string{"short_text", "Anything?", ""})

	if resp, page := postAccountStyle(t, app, creator, pictureForm("Corner Workshop"), ownLogo(t)); resp.StatusCode != http.StatusOK {
		t.Fatalf("saving the account's style: status %d\n%s", resp.StatusCode, page)
	}

	begun := mustGet(t, jarClient(t), app.Server.URL+"/s/"+id)
	if headerOf(begun) != "" || themeOf(t, begun) != "" {
		t.Fatalf("a published survey took the account's style before being published again")
	}
	editor := app.SurveyPage(t, creator, id)
	if !bodyContains(editor, "Your account style changed. Publish to apply it to this survey.") || !bodyContains(editor, "Publish version 2") {
		t.Fatalf("the editor does not say the account's style changed:\n%s", editor)
	}
	shown := preview(t, app, creator, id)
	if !bodyContains(headerOf(shown), "Corner Workshop") || themeOf(t, shown) != "ocean" {
		t.Fatalf("the preview does not show the account's style:\n%s", headerOf(shown))
	}
	logo := pictureSrc(headerOf(shown), regexp.MustCompile(`<img[^>]*>`))
	sha := logo[strings.LastIndex(logo, "/")+1:]
	if resp, _ := fetch(t, jarClient(t), app.Server.URL+"/style-image/"+sha); resp.StatusCode != http.StatusNotFound {
		t.Fatalf("the account's logo is public before a version shows it: status %d", resp.StatusCode)
	}

	// A respondent opened version 1; the survey is published again.
	respondent := jarClient(t)
	page := mustGet(t, respondent, app.Server.URL+"/s/"+id)
	form := respondForm(t, page)
	form.Set("q_"+extractAnswerFields(t, page)[0], "From version one")
	if body := app.Publish(t, creator, id); !bodyContains(body, "Published version 2") {
		t.Fatalf("publishing the account's style was refused:\n%s", body)
	}
	if editor := app.SurveyPage(t, creator, id); bodyContains(editor, "Your account style changed") {
		t.Errorf("the editor still says the account's style changed after publishing")
	}

	live := mustGet(t, jarClient(t), app.Server.URL+"/s/"+id)
	if !bodyContains(headerOf(live), "Corner Workshop") || themeOf(t, live) != "ocean" || !bodyContains(footerOf(live), "Corner Workshop Cooperative") {
		t.Fatalf("the published survey does not wear the account's style:\n%s", live)
	}
	if resp, _ := fetch(t, jarClient(t), app.Server.URL+"/style-image/"+sha); resp.StatusCode != http.StatusOK {
		t.Errorf("the account's logo is not public once a version shows it: status %d", resp.StatusCode)
	}

	_, thanks := submitAfterReadingTo(t, app, respondent, "/s/"+id, form)
	if headerOf(thanks) != "" || themeOf(t, thanks) != "" {
		t.Errorf("a response begun on version 1 was thanked in version 2's look")
	}

	// A further change to the account's style waits for the next publish.
	worded(t, app, creator, "Weekend classes.")
	if live := mustGet(t, jarClient(t), app.Server.URL+"/s/"+id); bodyContains(headerOf(live), "Weekend classes.") {
		t.Errorf("an account change reached a published version")
	}
}

// TestStyleAccount_TheStyleTabFollowsUntilAPartChanges: the Style tab
// shows the account's style; saving it unchanged keeps following, so a
// later account change reaches the preview; changing the tagline makes
// the header the survey's own, which a later account change leaves be.
func TestStyleAccount_TheStyleTabFollowsUntilAPartChanges(t *testing.T) {
	t.Parallel()
	app := apptest.New(t, apptest.Options{})
	creator := app.Login(t, apptest.UniqueEmail("style-account-tab"))
	accountWorkshop(t, app, creator)
	id := app.CreateSurvey(t, creator, "Following", true)
	app.AddQuestion(t, creator, id, "short_text", "Anything?", nil)

	tab := mustGet(t, creator, app.Server.URL+"/surveys/"+id+"/style")
	if !regexp.MustCompile(`value="ocean" checked`).MatchString(tab) || !bodyContains(tab, `value="Corner Workshop"`) {
		t.Fatalf("the Style tab does not show the account's style:\n%s", tab)
	}
	for i, got := range partSources(tab) {
		if got != "From your account style" {
			t.Errorf("part %d comes from %q, want the account", i, got)
		}
	}
	if !regexp.MustCompile(`name="custom_header" value="1" checked`).MatchString(tab) ||
		!regexp.MustCompile(`name="custom_footer" value="1" checked`).MatchString(tab) {
		t.Errorf("the header and footer boxes are not ticked by default")
	}
	if !bodyContains(tab, `href="/account/style"`) || !bodyContains(tab, "Reset to account style") {
		t.Errorf("the Style tab does not lead to the account's style or offer a reset")
	}

	saved := postStyleTab(t, app, creator, id, styleTabForm(workshopStyle()))
	for i, got := range partSources(saved) {
		if got != "From your account style" {
			t.Errorf("saving unchanged made part %d %q", i, got)
		}
	}
	worded(t, app, creator, "Weekend classes.")
	if !bodyContains(headerOf(preview(t, app, creator, id)), "Weekend classes.") {
		t.Errorf("a later account change did not reach a survey that saved its tab unchanged")
	}

	// The tab now shows the account's style as worded above.
	own := url.Values{
		"theme":             {"ocean"},
		"header_name":       {"Corner Workshop"},
		"header_tagline":    {"Our own tagline."},
		"header_link_label": {"Courses", "", ""},
		"header_link_url":   {"https://example.com/courses", "", ""},
		"footer_text":       {"Corner Workshop Ltd"},
	}
	saved = postStyleTab(t, app, creator, id, styleTabForm(own))
	if sources := partSources(saved); len(sources) < 3 || sources[1] != "This survey's own" || sources[2] != "From your account style" {
		t.Fatalf("changing the tagline: sources %q, want the header alone the survey's own", sources)
	}
	worded(t, app, creator, "Holiday classes.")
	header := headerOf(preview(t, app, creator, id))
	if !bodyContains(header, "Our own tagline.") || bodyContains(header, "Holiday classes.") {
		t.Errorf("a survey's own header followed the account:\n%s", header)
	}
}

// TestStyleAccount_HeaderAndFooterTurnedOff: an unticked box shows no
// header, or no footer, on the survey; with no header Earful's owl is
// back; ticking the box brings back what was there. A form drawn before
// the boxes existed changes neither.
func TestStyleAccount_HeaderAndFooterTurnedOff(t *testing.T) {
	t.Parallel()
	app := apptest.New(t, apptest.Options{})
	creator := app.Login(t, apptest.UniqueEmail("style-account-off"))
	if resp, page := postAccountStyle(t, app, creator, pictureForm("Corner Workshop"), ownLogo(t)); resp.StatusCode != http.StatusOK {
		t.Fatalf("saving the account's style: status %d\n%s", resp.StatusCode, page)
	}
	id := app.CreateSurvey(t, creator, "No header", true)
	app.AddQuestion(t, creator, id, "short_text", "Anything?", nil)

	off := styleTabForm(pictureForm("Corner Workshop"))
	off.Del("custom_header")
	off.Del("custom_footer")
	// A bad link in a header turned off cannot refuse the save.
	off["header_link_url"] = []string{"javascript:alert(1)", "", ""}
	tab := postStyleTab(t, app, creator, id, off)
	if regexp.MustCompile(`name="custom_header" value="1" checked`).MatchString(tab) {
		t.Errorf("the header box is still ticked after unticking it")
	}
	if !bodyContains(tab, `value="Corner Workshop"`) {
		t.Errorf("the tab lost the header it would turn back on")
	}
	page := preview(t, app, creator, id)
	if headerOf(page) != "" || footerOf(page) != "" {
		t.Errorf("a survey with its header and footer off still shows them")
	}
	if made := madeWithRe.FindString(page); !strings.Contains(made, "<svg") {
		t.Errorf("with no header, and so no logo, Earful's owl did not come back:\n%s", made)
	}

	// A form without the boxes leaves them as they are.
	postStyleTab(t, app, creator, id, url.Values{"theme": {"forest"}})
	if page := preview(t, app, creator, id); headerOf(page) != "" || themeOf(t, page) != "forest" {
		t.Errorf("a form drawn before the boxes existed turned the header back on, or lost the theme")
	}

	postStyleTab(t, app, creator, id, styleTabForm(pictureForm("Corner Workshop")))
	page = preview(t, app, creator, id)
	if !bodyContains(headerOf(page), "Corner Workshop") || footerOf(page) == "" {
		t.Errorf("ticking the boxes did not bring the header and footer back")
	}
	app.Publish(t, creator, id)
	if live := mustGet(t, jarClient(t), app.Server.URL+"/s/"+id); !bodyContains(headerOf(live), "Corner Workshop") {
		t.Errorf("the published survey has no header")
	}
}

// TestStyleAccount_ResetFollowsAgain: "Reset to account style" lets go of
// the survey's own parts and turns its header and footer back on, even
// with a problem in the form.
func TestStyleAccount_ResetFollowsAgain(t *testing.T) {
	t.Parallel()
	app := apptest.New(t, apptest.Options{})
	creator := app.Login(t, apptest.UniqueEmail("style-account-reset"))
	accountWorkshop(t, app, creator)
	id := app.CreateSurvey(t, creator, "Reset", true)
	app.AddQuestion(t, creator, id, "short_text", "Anything?", nil)

	own := styleTabForm(workshopStyle())
	own.Set("footer_text", "Our own footer")
	own.Set("theme", "forest")
	own.Del("custom_header")
	postStyleTab(t, app, creator, id, own)

	reset := styleTabForm(workshopStyle())
	reset.Set("style_action", "reset")
	reset["header_link_url"] = []string{"javascript:alert(1)", "", ""}
	page := postStyleTab(t, app, creator, id, reset)
	if !bodyContains(page, "Style reset.") {
		t.Fatalf("reset: no notice\n%s", page)
	}
	for i, got := range partSources(page) {
		if got != "From your account style" {
			t.Errorf("after a reset part %d comes from %q", i, got)
		}
	}
	shown := preview(t, app, creator, id)
	if themeOf(t, shown) != "ocean" || !bodyContains(footerOf(shown), "Corner Workshop Cooperative") || !bodyContains(headerOf(shown), "Corner Workshop") {
		t.Errorf("a reset survey is not drawn in the account's style")
	}
}

// TestStyleAccount_ATranslatedSurveyWaitsForTheAccount: a survey that
// shows the account's words in Spanish is not published in Spanish until
// the account's style is reviewed in Spanish, and is told where; once it
// is, a Spanish respondent reads the account's Spanish. A survey with a
// header and footer of its own does not wait.
func TestStyleAccount_ATranslatedSurveyWaitsForTheAccount(t *testing.T) {
	t.Parallel()
	app := apptest.New(t, apptest.Options{})
	creator := app.Login(t, apptest.UniqueEmail("style-account-es"))
	worded(t, app, creator, "Evening classes.")
	id := app.CreateSurvey(t, creator, "In Spanish", true)
	app.AddQuestion(t, creator, id, "short_text", "Why?", nil)
	identity := app.QuestionIdentities(t, creator, id)[0]
	app.PostForm(t, creator, "/surveys/"+id+"/localizations", url.Values{"lang": {"es"}}).Body.Close()
	app.PostForm(t, creator, "/surveys/"+id+"/localizations/es", url.Values{"t_" + identity: {"¿Por qué?"}}).Body.Close()

	languages := mustGet(t, creator, app.Server.URL+"/surveys/"+id+"/localizations")
	panel := languagePanel(t, languages, "es")
	if !strings.Contains(panel, "js-account-style-row") || !bodyContains(panel, "not yet reviewed in Spanish") ||
		!strings.Contains(panel, `href="/account/style/languages#lang-es"`) || bodyContains(panel, "Reviewed") {
		t.Errorf("the Languages tab does not show the account's words waiting in Spanish:\n%s", panel)
	}

	body := app.Publish(t, creator, id)
	if bodyContains(body, "Published version 1") || !bodyContains(body, "Your account style is not yet reviewed in Spanish") ||
		!strings.Contains(body, `href="/account/style/languages#lang-es"`) {
		t.Fatalf("publishing in a language the account is not reviewed in was not refused with the way there:\n%s", body)
	}

	if resp, page := postAccountTranslation(t, app, creator, "/account/style/languages/es", url.Values{
		"style_tagline":     {"Clases de tarde."},
		"style_header_link": {"Cursos"},
		"style_footer_text": {"Corner Workshop SL"},
	}); resp.StatusCode != http.StatusOK {
		t.Fatalf("reviewing the account's Spanish: status %d\n%s", resp.StatusCode, page)
	}
	if body := app.Publish(t, creator, id); !bodyContains(body, "Published version 1") {
		t.Fatalf("a survey whose account is reviewed in Spanish was refused:\n%s", body)
	}
	spanish := mustGet(t, jarClient(t), app.Server.URL+"/s/"+id+"?lang=es")
	if !bodyContains(headerOf(spanish), "Clases de tarde.") || !bodyContains(footerOf(spanish), "Corner Workshop SL") {
		t.Errorf("a Spanish respondent does not read the account's Spanish:\n%s", headerOf(spanish))
	}

	// The account's wording changes: the next publish waits again.
	worded(t, app, creator, "Weekend classes.")
	if body := app.Publish(t, creator, id); bodyContains(body, "Published version 2") {
		t.Errorf("a stale account translation was published")
	}

	// A survey with a header and footer of its own waits on its own
	// translation only.
	mine := app.CreateSurvey(t, creator, "Its own words", true)
	app.AddQuestion(t, creator, mine, "short_text", "Why?", nil)
	mineIdentity := app.QuestionIdentities(t, creator, mine)[0]
	own := styleTabForm(workshopStyle())
	own.Set("header_tagline", "Our tagline.")
	own.Set("footer_text", "Our footer.")
	postStyleTab(t, app, creator, mine, own)
	app.PostForm(t, creator, "/surveys/"+mine+"/localizations", url.Values{"lang": {"es"}}).Body.Close()
	app.PostForm(t, creator, "/surveys/"+mine+"/localizations/es", url.Values{
		"t_" + mineIdentity: {"¿Por qué?"},
		"style_tagline":     {"Nuestro lema."},
		"style_header_link": {"Nuestras clases", "Contacto"},
		"style_footer_text": {"Nuestro pie."},
		"style_footer_link": {"Aviso de privacidad"},
	}).Body.Close()
	if body := app.Publish(t, creator, mine); !bodyContains(body, "Published version 1") {
		t.Errorf("a survey with its own header and footer waited on the account's translation:\n%s", body)
	}
}

// TestStyleAccount_TheStarterSurveyFollowsToo: a workspace's Starter
// Survey follows the account's style like any other, and takes it when
// it is published again.
func TestStyleAccount_TheStarterSurveyFollowsToo(t *testing.T) {
	t.Parallel()
	app := apptest.New(t, apptest.Options{})
	creator := app.Login(t, apptest.UniqueEmail("style-account-starter"))
	if resp, page := postAccountStyle(t, app, creator, url.Values{"theme": {"forest"}}); resp.StatusCode != http.StatusOK {
		t.Fatalf("saving the account's theme: status %d\n%s", resp.StatusCode, page)
	}
	dashboard := mustGet(t, creator, app.Server.URL+"/surveys")
	m := regexp.MustCompile(`href="/surveys/([0-9a-f-]{36})"`).FindStringSubmatch(dashboard)
	if m == nil {
		t.Fatalf("no starter survey on the dashboard:\n%s", dashboard)
	}
	starter := m[1]
	if editor := app.SurveyPage(t, creator, starter); !bodyContains(editor, "Your account style changed") {
		t.Fatalf("the starter survey's editor does not say the account's style changed:\n%s", editor)
	}
	if body := app.Publish(t, creator, starter); !bodyContains(body, "Published version 2") {
		t.Fatalf("the starter survey was not published again:\n%s", body)
	}
	if got := themeOf(t, mustGet(t, jarClient(t), app.Server.URL+"/s/"+starter)); got != "forest" {
		t.Errorf("the starter survey's theme = %q, want forest", got)
	}
}

// TestStyleAccount_AnotherWorkspaceIsUntouched: an account's style is its
// own workspace's.
func TestStyleAccount_AnotherWorkspaceIsUntouched(t *testing.T) {
	t.Parallel()
	app := apptest.New(t, apptest.Options{})
	creator := app.Login(t, apptest.UniqueEmail("style-account-mine"))
	accountWorkshop(t, app, creator)
	stranger := app.Login(t, apptest.UniqueEmail("style-account-theirs"))
	theirs := app.CreateSurvey(t, stranger, "Theirs", true)
	app.AddQuestion(t, stranger, theirs, "short_text", "Anything?", nil)
	if page := preview(t, app, stranger, theirs); headerOf(page) != "" || themeOf(t, page) != "" {
		t.Errorf("another workspace's survey took this account's style")
	}
	if tab := mustGet(t, stranger, app.Server.URL+"/surveys/"+theirs+"/style"); bodyContains(tab, "Corner Workshop") || len(partSources(tab)) != 0 {
		t.Errorf("another workspace's Style tab shows this account's style")
	}
}

// TestStyleAccount_TheAccountPageSaysWhatTheStyleIs: the account page
// carries the account's style in a line, with the way to it.
func TestStyleAccount_TheAccountPageSaysWhatTheStyleIs(t *testing.T) {
	t.Parallel()
	app := apptest.New(t, apptest.Options{})
	creator := app.Login(t, apptest.UniqueEmail("style-account-card"))
	page := mustGet(t, creator, app.Server.URL+"/account")
	if !strings.Contains(page, "js-account-style-card") || !bodyContains(page, "Your surveys use Earful's own look") ||
		!strings.Contains(page, `href="/account/style"`) {
		t.Fatalf("the account page has no style card:\n%s", page)
	}
	accountWorkshop(t, app, creator)
	page = mustGet(t, creator, app.Server.URL+"/account")
	if !bodyContains(page, "Ocean theme, with a header and a footer.") || !strings.Contains(page, `href="/account/style/languages"`) {
		t.Errorf("the account page does not say what the style is:\n%s", page)
	}
}
