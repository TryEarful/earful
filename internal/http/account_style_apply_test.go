package http_test

import (
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/TryEarful/earful/internal/apptest"
)

// A change to the account's style reaches a published survey when it is
// next published (ADR-0023), or now, on the creator's word: each open
// survey it reaches goes out again from its live version, its draft left
// as it was.

var (
	staleSurveyRe = regexp.MustCompile(`(?s)<li class="js-stale-survey">.*?</li>`)
	hiddenFieldRe = regexp.MustCompile(`<input type="hidden" name="(survey|updated_at)" value="([^"]*)"`)
)

// staleTitles are the surveys the question names, in order.
func staleTitles(page string) []string {
	var out []string
	for _, item := range staleSurveyRe.FindAllString(page, -1) {
		title := regexp.MustCompile(`<a href="/surveys/[^"]*">([^<]*)</a>`).FindStringSubmatch(item)
		if title != nil {
			out = append(out, title[1])
		}
	}
	return out
}

// applyForm is the question's form as the page posts it.
func applyForm(t *testing.T, page string) url.Values {
	t.Helper()
	form := url.Values{}
	for _, m := range hiddenFieldRe.FindAllStringSubmatch(page, -1) {
		form.Add(m[1], m[2])
	}
	if form.Get("updated_at") == "" {
		t.Fatalf("the question has no form to apply the style with:\n%s", page)
	}
	return form
}

// saveFooter changes the account's footer to text, keeping the rest of
// the workshop style, and returns where the save led and what it showed.
func saveFooter(t *testing.T, app *apptest.App, creator *http.Client, text string) (string, string) {
	t.Helper()
	app.Clock.Advance(time.Second)
	form := workshopStyle()
	form.Set("footer_text", text)
	resp, page := postAccountStyle(t, app, creator, form)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("saving the account's style: status %d\n%s", resp.StatusCode, page)
	}
	return resp.Request.URL.Path, page
}

// withoutStarter is a creator whose workspace no longer holds its
// Starter Survey, which follows the account's style like any other and
// would be named in every question.
func withoutStarter(t *testing.T, app *apptest.App, address string) *http.Client {
	t.Helper()
	creator := app.Login(t, address)
	app.PostForm(t, creator, "/surveys/"+app.StarterSurveyID(t, creator)+"/delete", nil).Body.Close()
	return creator
}

func postApply(t *testing.T, app *apptest.App, creator *http.Client, form url.Values) (*http.Response, string) {
	t.Helper()
	resp := app.PostForm(t, creator, "/account/style/apply", form)
	defer resp.Body.Close()
	return resp, apptest.ReadBody(t, resp)
}

// TestAccountStyleApply_UpdatesTheOpenSurveysItReaches: changing the
// account's footer asks about the open surveys that show it, and no
// other. Updating them publishes each again with the new footer, and
// leaves what the creator has not published unpublished; a response
// begun before is thanked in the look it began in.
func TestAccountStyleApply_UpdatesTheOpenSurveysItReaches(t *testing.T) {
	t.Parallel()
	app := apptest.New(t, apptest.Options{})
	creator := withoutStarter(t, app, apptest.UniqueEmail("account-apply"))
	accountWorkshop(t, app, creator)

	follows := publishedSurvey(t, app, creator, "Follows the account", true, [3]string{"short_text", "Anything?", ""})
	ownFooter := app.CreateSurvey(t, creator, "Has its own footer", true)
	app.AddQuestion(t, creator, ownFooter, "short_text", "Why?", nil)
	own := styleTabForm(workshopStyle())
	own.Set("footer_text", "Its own footer")
	postStyleTab(t, app, creator, ownFooter, own)
	app.Publish(t, creator, ownFooter)
	closed := publishedSurvey(t, app, creator, "Closed already", true, [3]string{"short_text", "Why?", ""})
	app.PostForm(t, creator, "/surveys/"+closed+"/close", nil).Body.Close()

	// Another workspace's survey follows its own account.
	other := app.Login(t, apptest.UniqueEmail("account-apply-other"))
	accountWorkshop(t, app, other)
	otherSurvey := publishedSurvey(t, app, other, "Another workspace's", true, [3]string{"short_text", "Why?", ""})

	// A respondent opens the survey before the change.
	respondent := jarClient(t)
	begun := mustGet(t, respondent, app.Server.URL+"/s/"+follows)
	answers := respondForm(t, begun)
	answers.Set("q_"+extractAnswerFields(t, begun)[0], "Before the change")

	// The creator has an edit they have not published.
	app.AddQuestion(t, creator, follows, "short_text", "Not published yet", nil)

	path, page := saveFooter(t, app, creator, "Corner Workshop Cooperative\n14 Mill Lane")
	if path != "/account/style/apply" || !bodyContains(page, "Account style saved.") {
		t.Fatalf("saving a change open surveys show did not ask about them: at %s\n%s", path, page)
	}
	if got := staleTitles(page); len(got) != 1 || got[0] != "Follows the account" {
		t.Fatalf("the question names %q, want only the survey that shows the account's footer", got)
	}
	if !bodyContains(page, "Update this survey") || !bodyContains(page, "stay unpublished") {
		t.Errorf("the question does not offer the update or say what stays unpublished:\n%s", page)
	}

	resp, applied := postApply(t, app, creator, applyForm(t, page))
	if resp.StatusCode != http.StatusOK || resp.Request.URL.Path != "/account/style" || !bodyContains(applied, "1 survey updated.") {
		t.Fatalf("applying: status %d at %s\n%s", resp.StatusCode, resp.Request.URL.Path, applied)
	}

	live := mustGet(t, jarClient(t), app.Server.URL+"/s/"+follows)
	if !bodyContains(footerOf(live), "14 Mill Lane") {
		t.Errorf("the updated survey does not show the new footer:\n%s", footerOf(live))
	}
	if bodyContains(live, "Not published yet") {
		t.Errorf("updating the survey published an edit its creator had not published")
	}
	if editor := app.SurveyPage(t, creator, follows); !bodyContains(editor, "Publish version 3") {
		t.Errorf("the survey's draft no longer differs from what is live:\n%s", editor)
	}
	for _, id := range []string{ownFooter, closed} {
		if page := mustGet(t, jarClient(t), app.Server.URL+"/s/"+id); bodyContains(footerOf(page), "14 Mill Lane") {
			t.Errorf("survey %s was updated though the question did not name it", id)
		}
	}
	if page := mustGet(t, jarClient(t), app.Server.URL+"/s/"+otherSurvey); bodyContains(footerOf(page), "14 Mill Lane") {
		t.Errorf("another workspace's survey was updated")
	}

	_, thanks := submitAfterReadingTo(t, app, respondent, "/s/"+follows, answers)
	if bodyContains(footerOf(thanks), "14 Mill Lane") || !bodyContains(footerOf(thanks), "12 Mill Lane") {
		t.Errorf("a response begun before the update was thanked in the new look:\n%s", footerOf(thanks))
	}

	// With nothing left to ask about, the question goes back to the style.
	back, err := creator.Get(app.Server.URL + "/account/style/apply")
	if err != nil {
		t.Fatal(err)
	}
	back.Body.Close()
	if back.Request.URL.Path != "/account/style" {
		t.Errorf("the question with nothing to ask stayed at %s", back.Request.URL.Path)
	}

	// A change only to a footer no open survey takes from the account
	// saves without the question.
	app.Clock.Advance(time.Second)
	if body := app.Publish(t, creator, follows); !bodyContains(body, "Published version 3") {
		t.Fatalf("publishing the draft: %s", body)
	}
	postStyleTab(t, app, creator, follows, func() url.Values {
		f := styleTabForm(workshopStyle())
		f.Set("footer_text", "Its own footer too")
		return f
	}())
	app.Publish(t, creator, follows)
	if path, _ := saveFooter(t, app, creator, "Nobody shows this footer"); path != "/account/style" {
		t.Errorf("a change no open survey shows asked about surveys: at %s", path)
	}
}

// TestAccountStyleApply_LaterLeavesThemToTheirNextPublish: choosing later
// changes no survey, and the account's style page offers the update again.
func TestAccountStyleApply_LaterLeavesThemToTheirNextPublish(t *testing.T) {
	t.Parallel()
	app := apptest.New(t, apptest.Options{})
	creator := withoutStarter(t, app, apptest.UniqueEmail("account-apply-later"))
	accountWorkshop(t, app, creator)
	first := publishedSurvey(t, app, creator, "First", true, [3]string{"short_text", "Anything?", ""})
	publishedSurvey(t, app, creator, "Second", true, [3]string{"short_text", "Anything?", ""})

	_, page := saveFooter(t, app, creator, "A new address")
	if got := staleTitles(page); len(got) != 2 || got[0] != "First" || got[1] != "Second" {
		t.Fatalf("the question names %q, want both surveys in the order they were made", got)
	}
	if !bodyContains(page, "Update these 2 surveys") || !strings.Contains(page, `href="/account/style"`) {
		t.Errorf("the question does not offer both choices:\n%s", page)
	}

	style := mustGet(t, creator, app.Server.URL+"/account/style")
	if !bodyContains(style, "2 open surveys show an earlier version of your account style.") || !strings.Contains(style, `href="/account/style/apply"`) {
		t.Errorf("the account's style page does not offer the update again:\n%s", style)
	}
	if live := mustGet(t, jarClient(t), app.Server.URL+"/s/"+first); bodyContains(footerOf(live), "A new address") {
		t.Errorf("a survey changed though the creator chose later")
	}
	// Published in the ordinary way, a survey takes the change and is no
	// longer asked about.
	app.Publish(t, creator, first)
	if page := mustGet(t, creator, app.Server.URL+"/account/style/apply"); len(staleTitles(page)) != 1 {
		t.Errorf("a survey published since is still asked about: %q", staleTitles(page))
	}
}

// TestAccountStyleApply_ATranslationHoldsASurveyBack: a survey that went
// out in Spanish waits until the account's new words are read in Spanish,
// and is named with the way there; reviewing them asks again.
func TestAccountStyleApply_ATranslationHoldsASurveyBack(t *testing.T) {
	t.Parallel()
	app := apptest.New(t, apptest.Options{})
	creator := withoutStarter(t, app, apptest.UniqueEmail("account-apply-es"))
	worded(t, app, creator, "Evening classes.")
	reviewSpanish := func(tagline string) (*http.Response, string) {
		app.Clock.Advance(time.Second)
		return postAccountTranslation(t, app, creator, "/account/style/languages/es", url.Values{
			"style_tagline":     {tagline},
			"style_header_link": {"Cursos"},
			"style_footer_text": {"Corner Workshop SL"},
		})
	}
	id := app.CreateSurvey(t, creator, "In Spanish", true)
	app.AddQuestion(t, creator, id, "short_text", "Why?", nil)
	identity := app.QuestionIdentities(t, creator, id)[0]
	app.PostForm(t, creator, "/surveys/"+id+"/localizations", url.Values{"lang": {"es"}}).Body.Close()
	app.PostForm(t, creator, "/surveys/"+id+"/localizations/es", url.Values{"t_" + identity: {"¿Por qué?"}}).Body.Close()
	reviewSpanish("Clases de tarde.")
	if body := app.Publish(t, creator, id); !bodyContains(body, "Published version 1") {
		t.Fatalf("publishing in Spanish: %s", body)
	}

	app.Clock.Advance(time.Second)
	worded(t, app, creator, "Weekend classes.")
	page := mustGet(t, creator, app.Server.URL+"/account/style/apply")
	item := staleSurveyRe.FindString(page)
	if !bodyContains(item, "Needs your account style in Spanish first.") || !strings.Contains(item, `href="/account/style/languages#lang-es"`) {
		t.Fatalf("the survey waiting on Spanish is not named with the way there:\n%s", page)
	}
	if !bodyContains(item, "Waiting for Spanish") {
		t.Errorf("the survey waiting on Spanish has no chip saying so:\n%s", item)
	}
	if strings.Contains(page, "js-account-style-apply-form") || !(bodyContains(page, "No survey can be updated yet.") || bodyContains(page, "This survey can't be updated yet.")) || bodyContains(page, "Later leaves each survey") {
		t.Errorf("the question offers an update no survey can take:\n%s", page)
	}
	// The account's style page does not offer to update a survey that
	// cannot be updated yet: it offers to show what it needs.
	if style := mustGet(t, creator, app.Server.URL+"/account/style"); !bodyContains(style, "See what it needs") || bodyContains(style, "Update it") {
		t.Errorf("the account style page offers an update no survey can take:\n%s", style)
	}

	resp, reviewed := reviewSpanish("Clases de fin de semana.")
	if resp.Request.URL.Path != "/account/style/apply" || !bodyContains(reviewed, "Update this survey") {
		t.Fatalf("reviewing the Spanish did not ask about the survey: at %s\n%s", resp.Request.URL.Path, reviewed)
	}
	if resp, body := postApply(t, app, creator, applyForm(t, reviewed)); !bodyContains(body, "1 survey updated.") {
		t.Fatalf("applying: status %d\n%s", resp.StatusCode, body)
	}
	spanish := mustGet(t, jarClient(t), app.Server.URL+"/s/"+id+"?lang=es")
	if !bodyContains(headerOf(spanish), "Clases de fin de semana.") || !bodyContains(spanish, "¿Por qué?") {
		t.Errorf("the updated survey does not read the account's new Spanish:\n%s", headerOf(spanish))
	}
	// Asked again with nothing left, the question goes back to the
	// translation that was saved, still saying so.
	back, err := creator.Get(app.Server.URL + "/account/style/apply?saved=es")
	if err != nil {
		t.Fatal(err)
	}
	page = apptest.ReadBody(t, back)
	back.Body.Close()
	if back.Request.URL.Path != "/account/style/languages" || !bodyContains(page, "Saved and marked reviewed in Spanish.") {
		t.Errorf("with nothing left to ask, the question led to %s without its notice", back.Request.URL.Path)
	}
}

// TestAccountStyleApply_ASurveyWithNoRoomIsNamedNotOffered: a survey
// that keeps its limit of pictures, every one shown by a version, cannot
// take the account's new logo. The question names it with why, offers
// it nothing, and the other surveys still go out.
func TestAccountStyleApply_ASurveyWithNoRoomIsNamedNotOffered(t *testing.T) {
	t.Parallel()
	app := apptest.New(t, apptest.Options{})
	creator := withoutStarter(t, app, apptest.UniqueEmail("account-apply-full"))
	if resp, page := postAccountStyle(t, app, creator, accountStyleForm("The workshop's logo"), ownLogo(t)); resp.StatusCode != http.StatusOK {
		t.Fatalf("saving the account's style: status %d\n%s", resp.StatusCode, page)
	}

	// Each of nine versions thanks with a picture of its own: with the
	// account's logo, ten pictures, every one shown.
	full := app.CreateSurvey(t, creator, "Full of pictures", true)
	app.AddQuestion(t, creator, full, "short_text", "Why?", nil)
	for n := 1; n < 10; n++ {
		form := url.Values{"thanks_picture": {"image"}, "thanks_alt": {"Thank you " + strconv.Itoa(n)}}
		resp := app.PostMultipart(t, creator, "/surveys/"+full+"/style", form, ownThanksPicture(t))
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("thanks picture %d: status %d", n, resp.StatusCode)
		}
		app.Publish(t, creator, full)
	}
	room := publishedSurvey(t, app, creator, "Room to spare", true, [3]string{"short_text", "Why?", ""})

	app.Clock.Advance(time.Second)
	resp, page := postAccountStyle(t, app, creator, accountStyleForm("The workshop's new logo"), ownLogo(t))
	if resp.Request.URL.Path != "/account/style/apply" {
		t.Fatalf("a new logo did not ask about the open surveys: at %s\n%s", resp.Request.URL.Path, page)
	}
	var fullItem string
	for _, item := range staleSurveyRe.FindAllString(page, -1) {
		if strings.Contains(item, "Full of pictures") {
			fullItem = item
		}
	}
	if !bodyContains(fullItem, "No room for pictures") || !bodyContains(fullItem, "already holds 10 pictures") {
		t.Errorf("the full survey is not named with why it cannot be updated:\n%s", fullItem)
	}
	form := applyForm(t, page)
	if got := form["survey"]; len(got) != 1 || got[0] != room {
		t.Fatalf("the question offers %q, want only the survey with room", got)
	}

	// Asked about both, as a page drawn before the full survey filled up
	// would ask, neither stops the other.
	form["survey"] = []string{full, room}
	resp, applied := postApply(t, app, creator, form)
	if resp.StatusCode != http.StatusOK || !bodyContains(applied, "1 survey updated.") {
		t.Fatalf("applying with a full survey: status %d at %s\n%s", resp.StatusCode, resp.Request.URL.Path, applied)
	}
	if !bodyContains(applied, "No room for pictures") {
		t.Errorf("after applying, the full survey is not named as left behind:\n%s", applied)
	}
}

// TestAccountStyleApply_Guarded: a style changed since the question asks
// again; another workspace's survey, a forged form and a suspended
// workspace are refused, and nothing is published.
func TestAccountStyleApply_Guarded(t *testing.T) {
	t.Parallel()
	app := apptest.New(t, apptest.Options{})
	address := apptest.UniqueEmail("account-apply-guarded")
	creator := withoutStarter(t, app, address)
	accountWorkshop(t, app, creator)
	id := publishedSurvey(t, app, creator, "Guarded", true, [3]string{"short_text", "Anything?", ""})
	_, page := saveFooter(t, app, creator, "First change")
	asked := applyForm(t, page)

	// The style changes again before the creator answers.
	saveFooter(t, app, creator, "Second change")
	resp, body := postApply(t, app, creator, asked)
	if resp.Request.URL.Path != "/account/style/apply" || !bodyContains(body, "Your account style changed since you were asked.") {
		t.Fatalf("a style changed since the question was applied unseen: at %s\n%s", resp.Request.URL.Path, body)
	}
	if live := mustGet(t, jarClient(t), app.Server.URL+"/s/"+id); bodyContains(footerOf(live), "change") {
		t.Fatalf("a survey was updated with a style the creator was not asked about")
	}
	fresh := applyForm(t, body)

	// Another workspace's survey.
	other := app.Login(t, apptest.UniqueEmail("account-apply-intruder"))
	otherID := publishedSurvey(t, app, other, "Not yours", true, [3]string{"short_text", "Why?", ""})
	forged := url.Values{"updated_at": {fresh.Get("updated_at")}, "survey": {id, otherID}}
	if resp, _ := postApply(t, app, creator, forged); resp.StatusCode != http.StatusNotFound {
		t.Errorf("another workspace's survey: status %d, want 404", resp.StatusCode)
	}

	// A form with no CSRF token.
	plain, err := creator.PostForm(app.Server.URL+"/account/style/apply", url.Values{"updated_at": {fresh.Get("updated_at")}, "survey": {id}})
	if err != nil {
		t.Fatal(err)
	}
	plain.Body.Close()
	if plain.StatusCode != http.StatusForbidden {
		t.Errorf("a forged form: status %d, want 403", plain.StatusCode)
	}

	// A suspended workspace publishes nothing.
	suspendWorkspace(t, app, address, "Impersonating a bank")
	if resp, body := postApply(t, app, creator, fresh); resp.StatusCode != http.StatusForbidden || !bodyContains(body, "Your surveys weren't updated.") {
		t.Errorf("a suspended workspace: status %d\n%s", resp.StatusCode, body)
	}
	if live := mustGet(t, jarClient(t), app.Server.URL+"/s/"+id); bodyContains(footerOf(live), "change") {
		t.Errorf("a refused update published the survey")
	}
}
