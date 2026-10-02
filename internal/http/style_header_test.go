package http_test

import (
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"testing"

	"github.com/TryEarful/earful/internal/apptest"
	"github.com/TryEarful/earful/internal/export"
)

// A survey's own header and footer (ADR-0018): a name, a tagline, links
// and a footer text, set on the Style tab with the theme.

// styleForm is the Style tab's form as the page posts it: every field of
// every part, whatever is in it.
func styleForm(theme string) url.Values {
	return url.Values{
		"theme":             {theme},
		"header_name":       {""},
		"header_tagline":    {""},
		"header_link_label": {"", "", ""},
		"header_link_url":   {"", "", ""},
		"footer_text":       {""},
		"footer_link_label": {"", "", ""},
		"footer_link_url":   {"", "", ""},
	}
}

// workshopStyle is a form with every part filled.
func workshopStyle() url.Values {
	form := styleForm("ocean")
	form.Set("header_name", "Corner Workshop")
	form.Set("header_tagline", "Evening classes in wood and clay.\nTell us how it went.")
	form["header_link_label"] = []string{"Our classes", "", "Contact"}
	form["header_link_url"] = []string{"https://example.com/classes", "", "https://example.com/contact"}
	form.Set("footer_text", "Corner Workshop Cooperative\n12 Mill Lane")
	form["footer_link_label"] = []string{"Privacy notice", "", ""}
	form["footer_link_url"] = []string{"https://example.com/privacy", "", ""}
	return form
}

func postStyle(t *testing.T, app *apptest.App, creator *http.Client, id string, form url.Values) (*http.Response, string) {
	t.Helper()
	resp := app.PostForm(t, creator, "/surveys/"+id+"/style", form)
	defer resp.Body.Close()
	return resp, apptest.ReadBody(t, resp)
}

var (
	styleHeaderRe = regexp.MustCompile(`(?s)<header class="[^"]*\bjs-style-header\b[^"]*">.*?</header>`)
	styleFooterRe = regexp.MustCompile(`(?s)<div class="[^"]*\bjs-style-footer\b[^"]*">.*?</div>`)
	headingRe     = regexp.MustCompile(`<h([1-6])[ >]`)
)

// headerOf is the survey's own header on a page, or empty where the page
// has none.
func headerOf(page string) string { return styleHeaderRe.FindString(page) }

// footerOf is the creator's footer on a page, or empty.
func footerOf(page string) string { return styleFooterRe.FindString(page) }

// TestStyleHeader_PreviewedThenPublished: the header and the footer are
// the draft's, so the preview has them at once and respondents only once
// a version is published with them. The questions page shows the whole
// header; the pages after it show the name alone; every one shows the
// footer.
func TestStyleHeader_PreviewedThenPublished(t *testing.T) {
	t.Parallel()
	app := apptest.New(t, apptest.Options{})
	creator := app.Login(t, apptest.UniqueEmail("style-header"))
	id := app.CreateSurvey(t, creator, "Open day", true)
	app.AddQuestion(t, creator, id, "short_text", "Anything?", nil)
	app.Publish(t, creator, id)

	resp, saved := postStyle(t, app, creator, id, workshopStyle())
	if resp.StatusCode != http.StatusOK || !bodyContains(saved, "Style saved") {
		t.Fatalf("saving a header: status %d\n%s", resp.StatusCode, saved)
	}
	// The tab shows what was saved, the empty row between two links closed up.
	for _, want := range []string{
		`name="header_name" value="Corner Workshop"`,
		`name="header_link_label" value="Contact"`,
		`name="footer_link_url" value="https://example.com/privacy"`,
		"Tell us how it went.",
		"12 Mill Lane",
	} {
		if !strings.Contains(saved, want) {
			t.Errorf("the Style tab does not show %q again:\n%s", want, saved)
		}
	}
	// The creator's own page wears none of it.
	if headerOf(saved) != "" || footerOf(saved) != "" {
		t.Errorf("the Style tab itself has the survey's header or footer")
	}

	preview := mustGet(t, creator, app.Server.URL+"/surveys/"+id+"/preview")
	header := headerOf(preview)
	for _, want := range []string{"Corner Workshop", "Evening classes in wood and clay.", "Tell us how it went.", "Our classes", "Contact"} {
		if !bodyContains(header, want) {
			t.Errorf("the preview's header lacks %q:\n%s", want, header)
		}
	}
	if !bodyContains(footerOf(preview), "Corner Workshop Cooperative") || !bodyContains(footerOf(preview), "Privacy notice") {
		t.Errorf("the preview's footer is not the creator's:\n%s", footerOf(preview))
	}

	live := mustGet(t, jarClient(t), app.Server.URL+"/s/"+id)
	if headerOf(live) != "" || footerOf(live) != "" {
		t.Fatalf("the draft's header reached respondents before it was published:\n%s", live)
	}
	if body := app.Publish(t, creator, id); !bodyContains(body, "Published version 2") {
		t.Fatalf("publishing the header was refused:\n%s", body)
	}

	live = mustGet(t, jarClient(t), app.Server.URL+"/s/"+id)
	header = headerOf(live)
	if !bodyContains(header, "Corner Workshop") || !bodyContains(header, "Evening classes") {
		t.Fatalf("the published header is missing:\n%s", live)
	}
	// A link opens in a new tab, tells the page it opens nothing, and
	// says so to a screen reader.
	if !regexp.MustCompile(`<a class="js-style-link" href="https://example.com/classes" target="_blank" rel="noopener noreferrer">`).MatchString(header) {
		t.Errorf("a header link is not a safe link to a new tab:\n%s", header)
	}
	if !bodyContains(header, "(opens in a new tab)") {
		t.Errorf("a header link does not say it opens in a new tab:\n%s", header)
	}
	if !bodyContains(footerOf(live), "12 Mill Lane") ||
		!strings.Contains(footerOf(live), `href="https://example.com/privacy"`) {
		t.Errorf("the published footer is missing:\n%s", live)
	}
	// The disclosure is Earful's, in Earful's words, whatever the header says.
	if !strings.Contains(live, "js-disclosure") {
		t.Errorf("the disclosure is gone from a styled survey:\n%s", live)
	}
	if !bodyContains(live, "Powered by Earful") {
		t.Errorf("the footer does not say what the survey runs on:\n%s", live)
	}

	// After the questions the header is the name alone, and the footer stays.
	thanks := answerOnce(t, app, "/s/"+id, "hello")
	if header := headerOf(thanks); !bodyContains(header, "Corner Workshop") || bodyContains(header, "Evening classes") || bodyContains(header, "Our classes") {
		t.Errorf("the thanks page's header is not the name alone:\n%s", header)
	}
	if !bodyContains(footerOf(thanks), "Privacy notice") {
		t.Errorf("the thanks page lost the creator's footer:\n%s", thanks)
	}

	app.PostForm(t, creator, "/surveys/"+id+"/close", nil).Body.Close()
	closed := mustGetStatus(t, jarClient(t), app.Server.URL+"/s/"+id, http.StatusGone)
	if header := headerOf(closed); !bodyContains(header, "Corner Workshop") || bodyContains(header, "Evening classes") {
		t.Errorf("the closed page's header is not the name alone:\n%s", closed)
	}
	if !bodyContains(footerOf(closed), "12 Mill Lane") {
		t.Errorf("the closed page lost the creator's footer:\n%s", closed)
	}

	// A link that leads to no survey has nobody's header.
	missing := mustGetStatus(t, jarClient(t), app.Server.URL+"/s/00000000-0000-0000-0000-000000000000", http.StatusNotFound)
	if headerOf(missing) != "" || footerOf(missing) != "" {
		t.Errorf("a missing survey's page has a header or footer:\n%s", missing)
	}
}

// mustGetStatus reads a page that answers with a status other than 200.
func mustGetStatus(t *testing.T, client *http.Client, address string, want int) string {
	t.Helper()
	resp, err := client.Get(address)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != want {
		t.Fatalf("GET %s: status %d, want %d", address, resp.StatusCode, want)
	}
	return apptest.ReadBody(t, resp)
}

// TestStyleHeader_HeadingOrderHolds: the name is not a heading, so the
// page's first heading is its one <h1>, the survey's title, with a
// header and without one.
func TestStyleHeader_HeadingOrderHolds(t *testing.T) {
	t.Parallel()
	app := apptest.New(t, apptest.Options{})
	creator := app.Login(t, apptest.UniqueEmail("style-headings"))
	id := app.CreateSurvey(t, creator, "Heading order", true)
	app.AddQuestion(t, creator, id, "short_text", "Anything?", nil)

	check := func(when string) {
		t.Helper()
		page := mustGet(t, creator, app.Server.URL+"/surveys/"+id+"/preview")
		levels := headingRe.FindAllStringSubmatch(page, -1)
		if len(levels) == 0 || levels[0][1] != "1" {
			t.Fatalf("%s: the page's first heading is not its <h1>: %v", when, levels)
		}
		ones := 0
		for _, level := range levels {
			if level[1] == "1" {
				ones++
			}
		}
		if ones != 1 {
			t.Errorf("%s: the page has %d <h1>, want one", when, ones)
		}
		if headingRe.MatchString(headerOf(page)) {
			t.Errorf("%s: the survey's header holds a heading:\n%s", when, headerOf(page))
		}
		if !regexp.MustCompile(`<h1>Heading order</h1>`).MatchString(page) {
			t.Errorf("%s: the <h1> is not the survey's title", when)
		}
	}
	check("without a header")
	postStyle(t, app, creator, id, workshopStyle())
	check("with a header")
}

// TestStyleHeader_ThemeAndHeaderAreSavedTogether: the tab is one form,
// and a form that carries only some of a style's parts leaves the others
// as they were, so a part is never emptied by a form that does not draw
// it.
func TestStyleHeader_ThemeAndHeaderAreSavedTogether(t *testing.T) {
	t.Parallel()
	app := apptest.New(t, apptest.Options{})
	creator := app.Login(t, apptest.UniqueEmail("style-merge"))
	id := app.CreateSurvey(t, creator, "Merged style", true)
	app.AddQuestion(t, creator, id, "short_text", "Anything?", nil)
	postStyle(t, app, creator, id, workshopStyle())

	// The theme alone.
	saveStyle(t, app, creator, id, "forest")
	preview := mustGet(t, creator, app.Server.URL+"/surveys/"+id+"/preview")
	if got := themeOf(t, preview); got != "forest" {
		t.Errorf("theme = %q, want forest", got)
	}
	if !bodyContains(headerOf(preview), "Corner Workshop") || !bodyContains(footerOf(preview), "Privacy notice") {
		t.Errorf("saving the theme alone dropped the header or the footer:\n%s", preview)
	}

	// The header alone.
	postStyle(t, app, creator, id, url.Values{"header_name": {"Riverton Pottery"}})
	preview = mustGet(t, creator, app.Server.URL+"/surveys/"+id+"/preview")
	if got := themeOf(t, preview); got != "forest" {
		t.Errorf("saving the header alone changed the theme to %q", got)
	}
	if header := headerOf(preview); !bodyContains(header, "Riverton Pottery") || bodyContains(header, "Our classes") {
		t.Errorf("the header is not the one saved:\n%s", header)
	}
	if !bodyContains(footerOf(preview), "Privacy notice") {
		t.Errorf("saving the header alone dropped the footer:\n%s", preview)
	}

	// The whole form, emptied, is no header and no footer at all.
	postStyle(t, app, creator, id, styleForm("forest"))
	preview = mustGet(t, creator, app.Server.URL+"/surveys/"+id+"/preview")
	if headerOf(preview) != "" || footerOf(preview) != "" {
		t.Errorf("an emptied form left a header or a footer:\n%s", preview)
	}
}

// TestStyleHeader_RefusesWhatItShouldNot: each refusal comes with a
// message that says where the problem is, shows what was typed again,
// and leaves the draft as it was.
func TestStyleHeader_RefusesWhatItShouldNot(t *testing.T) {
	t.Parallel()
	app := apptest.New(t, apptest.Options{})
	creator := app.Login(t, apptest.UniqueEmail("style-refusals"))
	id := app.CreateSurvey(t, creator, "Refusals", true)
	app.AddQuestion(t, creator, id, "short_text", "Anything?", nil)
	postStyle(t, app, creator, id, workshopStyle())

	cases := []struct {
		name   string
		change func(url.Values)
		want   string
	}{
		{"a script address", func(f url.Values) {
			f["header_link_url"] = []string{"javascript:alert(1)", "", ""}
		}, "Header link 1: the link address must start with http:// or https://"},
		{"a relative address", func(f url.Values) {
			f["footer_link_url"] = []string{"/surveys", "", ""}
		}, "Footer link 1: the link address must start with http:// or https://"},
		{"a label with no address", func(f url.Values) {
			f["header_link_label"] = []string{"Our classes", "Shop", "Contact"}
		}, "Header link 2: add the address the link goes to"},
		{"an address with no label", func(f url.Values) {
			f["footer_link_label"] = []string{"", "", ""}
		}, "Footer link 1: give the link a label"},
		{"a fourth link", func(f url.Values) {
			f["header_link_label"] = []string{"One", "Two", "Three", "Four"}
			f["header_link_url"] = []string{"https://example.com/1", "https://example.com/2", "https://example.com/3", "https://example.com/4"}
		}, "Header links: add at most 3 links"},
		{"a name too long", func(f url.Values) {
			f.Set("header_name", strings.Repeat("n", 81))
		}, "Name: keep the name under 80 characters"},
		{"a tagline too long", func(f url.Values) {
			f.Set("header_tagline", strings.Repeat("t", 281))
		}, "Tagline: keep the tagline under 280 characters"},
		{"a footer too long", func(f url.Values) {
			f.Set("footer_text", strings.Repeat("f", 281))
		}, "Footer text: keep the footer text under 280 characters"},
		{"a label too long", func(f url.Values) {
			f["footer_link_label"] = []string{strings.Repeat("l", 61), "", ""}
		}, "Footer link 1: keep the link label under 60 characters"},
	}
	for _, tc := range cases {
		form := workshopStyle()
		form.Set("header_name", "Typed "+tc.name)
		tc.change(form)
		resp, body := postStyle(t, app, creator, id, form)
		if resp.StatusCode != http.StatusUnprocessableEntity {
			t.Errorf("%s: status = %d, want 422", tc.name, resp.StatusCode)
		}
		if !bodyContains(body, tc.want) {
			t.Errorf("%s: no message says %q:\n%s", tc.name, tc.want, body)
		}
		if !strings.Contains(body, "js-style-field-error") {
			t.Errorf("%s: the problem is not shown beside its field:\n%s", tc.name, body)
		}
		// A problem with one field marks that field; too many links is
		// no one field's.
		if marked := strings.Contains(body, `aria-invalid="true"`); marked == (tc.name == "a fourth link") {
			t.Errorf("%s: a field marked as invalid = %v:\n%s", tc.name, marked, body)
		}
		if !strings.Contains(body, "Typed "+tc.name) && !strings.Contains(body, strings.Repeat("n", 81)) {
			t.Errorf("%s: what was typed is not shown again:\n%s", tc.name, body)
		}
		preview := mustGet(t, creator, app.Server.URL+"/surveys/"+id+"/preview")
		if header := headerOf(preview); !bodyContains(header, "Corner Workshop") || bodyContains(header, "Typed") {
			t.Errorf("%s: a refused style changed the draft:\n%s", tc.name, header)
		}
	}
}

// TestStyleHeader_IsPlainText: nothing a creator types is read as
// markup on a respondent's page.
func TestStyleHeader_IsPlainText(t *testing.T) {
	t.Parallel()
	app := apptest.New(t, apptest.Options{})
	creator := app.Login(t, apptest.UniqueEmail("style-plain"))
	id := app.CreateSurvey(t, creator, "Plain text", true)
	app.AddQuestion(t, creator, id, "short_text", "Anything?", nil)

	form := styleForm("earful")
	form.Set("header_name", `<img src=x onerror=alert(1)>`)
	form.Set("header_tagline", `<script>alert(2)</script>`)
	form["header_link_label"] = []string{`<b>bold</b>`, "", ""}
	form["header_link_url"] = []string{`https://example.com/?a="><script>alert(3)</script>`, "", ""}
	form.Set("footer_text", `</footer><script>alert(4)</script>`)
	if resp, body := postStyle(t, app, creator, id, form); resp.StatusCode != http.StatusOK {
		t.Fatalf("saving: status %d\n%s", resp.StatusCode, body)
	}
	app.Publish(t, creator, id)

	live := mustGet(t, jarClient(t), app.Server.URL+"/s/"+id)
	for _, raw := range []string{"<img src=x", "<script>alert", "<b>bold</b>", "</footer><script>"} {
		if strings.Contains(live, raw) {
			t.Errorf("the page carries %q as markup:\n%s", raw, live)
		}
	}
	if !bodyContains(headerOf(live), `<img src=x onerror=alert(1)>`) || !bodyContains(headerOf(live), "<b>bold</b>") {
		t.Errorf("the typed text is not shown as text:\n%s", headerOf(live))
	}
}

// TestStyleHeader_FrozenPerVersion is ADR-0001 for the header: a
// respondent who was served version 1 is thanked under version 1's
// header, whatever was published while they were answering.
func TestStyleHeader_FrozenPerVersion(t *testing.T) {
	t.Parallel()
	app := apptest.New(t, apptest.Options{})
	creator := app.Login(t, apptest.UniqueEmail("style-header-frozen"))
	id := app.CreateSurvey(t, creator, "Frozen header", true)
	app.AddQuestion(t, creator, id, "short_text", "Anything?", nil)
	postStyle(t, app, creator, id, workshopStyle())
	app.Publish(t, creator, id)

	respondent := jarClient(t)
	page := mustGet(t, respondent, app.Server.URL+"/s/"+id)
	form := respondForm(t, page)
	form.Set("q_"+extractAnswerFields(t, page)[0], "from version 1")

	renamed := workshopStyle()
	renamed.Set("header_name", "Riverton Pottery")
	postStyle(t, app, creator, id, renamed)
	if body := app.Publish(t, creator, id); !bodyContains(body, "Published version 2") {
		t.Fatalf("a changed header alone could not be published:\n%s", body)
	}

	resp, thanks := submitAfterReading(t, app, respondent, id, form)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("submit: status %d\n%s", resp.StatusCode, thanks)
	}
	if header := headerOf(thanks); !bodyContains(header, "Corner Workshop") || bodyContains(header, "Riverton Pottery") {
		t.Errorf("a response to version 1 was thanked under another version's header:\n%s", header)
	}
	if header := headerOf(mustGet(t, jarClient(t), app.Server.URL+"/s/"+id)); !bodyContains(header, "Riverton Pottery") {
		t.Errorf("version 2's header is not the new one:\n%s", header)
	}
	// Saving what is already live is not a change.
	postStyle(t, app, creator, id, renamed)
	if editor := app.SurveyPage(t, creator, id); bodyContains(editor, "Publish version 3") {
		t.Errorf("an unchanged header offers a publish:\n%s", editor)
	}
}

// TestStyleHeader_Localized: the tagline, the footer's text and the
// links' labels are translated with the questions and reviewed like
// them; the name and the addresses are the same in every language. A
// respondent reading Dutch reads them in Dutch, and one reading a
// language the survey has no translation into reads them as written.
func TestStyleHeader_Localized(t *testing.T) {
	t.Parallel()
	app := apptest.New(t, apptest.Options{AI: translatorFake()})
	creator := app.Login(t, apptest.UniqueEmail("style-lang"))
	id := app.CreateSurvey(t, creator, "Localized style", true)
	app.AddQuestion(t, creator, id, "short_text", "Why?", nil)
	identity := app.QuestionIdentities(t, creator, id)[0]
	postStyle(t, app, creator, id, workshopStyle())
	app.PostForm(t, creator, "/surveys/"+id+"/localizations", url.Values{"lang": {"nl"}}).Body.Close()
	app.PostForm(t, creator, "/surveys/"+id+"/localizations/nl", url.Values{
		"t_" + identity: {"Waarom?"},
	}).Body.Close()

	if body := app.Publish(t, creator, id); bodyContains(body, "Published version 1") {
		t.Fatalf("a language without its header and footer was published:\n%s", body)
	}
	languages := mustGet(t, creator, app.Server.URL+"/surveys/"+id+"/localizations")
	if !bodyContains(languages, "1 of 2 reviewed") {
		t.Errorf("the progress does not count the header and footer:\n%s", languages)
	}
	// The name and the addresses are not offered for translation.
	translation := regexp.MustCompile(`(?s)<div class="js-style-translation">.*?</div>`).FindString(languages)
	if translation == "" || bodyContains(translation, "Corner Workshop<") || strings.Contains(translation, "example.com") {
		t.Errorf("the languages page offers the wrong parts of the style:\n%s", languages)
	}
	for _, want := range []string{"Evening classes in wood and clay.", "Our classes", "Contact", "Corner Workshop Cooperative", "Privacy notice"} {
		if !bodyContains(translation, want) {
			t.Errorf("the languages page does not show %q to translate:\n%s", want, translation)
		}
	}

	// The model drafts them; a draft is not a review.
	resp := app.PostForm(t, creator, "/surveys/"+id+"/localizations/nl/draft", nil)
	drafted := apptest.ReadBody(t, resp)
	resp.Body.Close()
	if !regexp.MustCompile(`name="style_tagline"[^>]*>\[vertaald\]`).MatchString(drafted) {
		t.Fatalf("the tagline was not drafted:\n%s", drafted)
	}
	if body := app.Publish(t, creator, id); bodyContains(body, "Published version 1") {
		t.Fatalf("an unreviewed header was published:\n%s", body)
	}

	app.PostForm(t, creator, "/surveys/"+id+"/localizations/nl", url.Values{
		"t_" + identity:     {"Waarom?"},
		"style_tagline":     {"Avondlessen in hout en klei."},
		"style_header_link": {"Onze lessen", "Contact opnemen"},
		"style_footer_text": {"Cooperatie Corner Workshop"},
		"style_footer_link": {"Privacyverklaring"},
	}).Body.Close()
	if body := app.Publish(t, creator, id); !bodyContains(body, "Published version 1") {
		t.Fatalf("a reviewed language was refused:\n%s", body)
	}

	dutch := mustGet(t, jarClient(t), app.Server.URL+"/s/"+id+"?lang=nl")
	header, footer := headerOf(dutch), footerOf(dutch)
	for _, want := range []string{"Corner Workshop", "Avondlessen in hout en klei.", "Onze lessen", "Contact opnemen"} {
		if !bodyContains(header, want) {
			t.Errorf("the Dutch header lacks %q:\n%s", want, header)
		}
	}
	if bodyContains(header, "Evening classes") || bodyContains(header, "Our classes") {
		t.Errorf("the Dutch header shows the original words:\n%s", header)
	}
	if !strings.Contains(header, `href="https://example.com/classes"`) || !strings.Contains(header, `href="https://example.com/contact"`) {
		t.Errorf("the Dutch header lost its addresses:\n%s", header)
	}
	if !bodyContains(footer, "Cooperatie Corner Workshop") || !bodyContains(footer, "Privacyverklaring") ||
		!strings.Contains(footer, `href="https://example.com/privacy"`) {
		t.Errorf("the Dutch footer is not the translation:\n%s", footer)
	}
	// The thanks page keeps the language of the form it answers.
	if thanks := answerOnce(t, app, "/s/"+id+"?lang=nl", "Omdat"); !bodyContains(footerOf(thanks), "Privacyverklaring") {
		t.Errorf("the Dutch thanks page's footer is not in Dutch:\n%s", footerOf(thanks))
	}

	// As written, and in a language the survey was not translated into.
	for _, address := range []string{"/s/" + id, "/s/" + id + "?lang=es"} {
		page := mustGet(t, jarClient(t), app.Server.URL+address)
		if !bodyContains(headerOf(page), "Evening classes in wood and clay.") || !bodyContains(footerOf(page), "Privacy notice") {
			t.Errorf("%s does not show the header and footer as written:\n%s", address, headerOf(page))
		}
	}

	// Rewording the tagline sends the translation back for review.
	reworded := workshopStyle()
	reworded.Set("header_tagline", "Classes most evenings.")
	postStyle(t, app, creator, id, reworded)
	languages = mustGet(t, creator, app.Server.URL+"/surveys/"+id+"/localizations")
	if !bodyContains(languages, "The header or footer changed after this was translated") {
		t.Errorf("a stale translation is not marked:\n%s", languages)
	}
	if body := app.Publish(t, creator, id); bodyContains(body, "Published version 2") {
		t.Errorf("a stale header translation was published:\n%s", body)
	}
}

// TestStyleHeader_CrossWorkspaceDenied: another workspace's survey
// cannot be given a header.
func TestStyleHeader_CrossWorkspaceDenied(t *testing.T) {
	t.Parallel()
	app := apptest.New(t, apptest.Options{})
	owner := app.Login(t, apptest.UniqueEmail("style-header-owner"))
	id := app.CreateSurvey(t, owner, "Somebody else's", true)
	app.AddQuestion(t, owner, id, "short_text", "Anything?", nil)
	intruder := app.Login(t, apptest.UniqueEmail("style-header-intruder"))

	if resp, _ := postStyle(t, app, intruder, id, workshopStyle()); resp.StatusCode != http.StatusNotFound {
		t.Errorf("styling another workspace's survey: status = %d, want 404", resp.StatusCode)
	}
	if preview := mustGet(t, owner, app.Server.URL+"/surveys/"+id+"/preview"); headerOf(preview) != "" || footerOf(preview) != "" {
		t.Errorf("the owner's survey was given a header by somebody else:\n%s", headerOf(preview))
	}
}

// TestStyleHeader_Exported: the workspace export carries each version's
// header and footer as that version was published with them.
func TestStyleHeader_Exported(t *testing.T) {
	t.Parallel()
	app := apptest.New(t, apptest.Options{})
	creator := app.Login(t, apptest.UniqueEmail("style-header-export"))
	id := app.CreateSurvey(t, creator, "Exported header", true)
	app.AddQuestion(t, creator, id, "short_text", "Anything?", nil)
	saveStyle(t, app, creator, id, "slate")
	app.Publish(t, creator, id)
	postStyle(t, app, creator, id, workshopStyle())
	app.Publish(t, creator, id)

	link := waitForExport(t, app, creator)
	resp, err := creator.Get(app.Server.URL + link)
	if err != nil {
		t.Fatalf("download: %v", err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read archive: %v", err)
	}
	var archive export.Archive
	if err := json.Unmarshal(openArchive(t, raw)["workspace.json"], &archive); err != nil {
		t.Fatalf("workspace.json: %v", err)
	}
	for _, survey := range archive.Surveys {
		if survey.ID != id {
			continue
		}
		if len(survey.Versions) != 2 {
			t.Fatalf("versions = %d, want 2", len(survey.Versions))
		}
		for _, version := range survey.Versions {
			style := version.Style
			if style == nil {
				t.Fatalf("version %d has no style", version.Number)
			}
			switch version.Number {
			case 1:
				if style.Theme != "slate" || style.Header != nil || style.Footer != nil {
					t.Errorf("version 1 style = %+v, want the theme alone", style)
				}
			case 2:
				if style.Theme != "ocean" || style.Header == nil || style.Footer == nil {
					t.Fatalf("version 2 style = %+v, want a theme, a header and a footer", style)
				}
				if style.Header.Name != "Corner Workshop" ||
					style.Header.Tagline != "Evening classes in wood and clay.\nTell us how it went." ||
					len(style.Header.Links) != 2 || style.Header.Links[1] != (export.StyleLink{Label: "Contact", URL: "https://example.com/contact"}) {
					t.Errorf("version 2 header = %+v", style.Header)
				}
				if style.Footer.Text != "Corner Workshop Cooperative\n12 Mill Lane" ||
					len(style.Footer.Links) != 1 || style.Footer.Links[0].URL != "https://example.com/privacy" {
					t.Errorf("version 2 footer = %+v", style.Footer)
				}
			}
		}
		return
	}
	t.Fatalf("the survey is missing from the export")
}
