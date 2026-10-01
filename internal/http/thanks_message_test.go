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

const (
	thanksMessageHook = "js-thanks-message"
	thanksLinkHook    = "js-thanks-link"
	defaultThanks     = "Your answers are in. You can close this page now."
)

// saveThanks posts the editor's thank you page form.
func saveThanks(t *testing.T, app *apptest.App, creator *http.Client, id, message, label, link string) (*http.Response, string) {
	t.Helper()
	resp := app.PostForm(t, creator, "/surveys/"+id+"/thanks", url.Values{
		"thanks_message":    {message},
		"thanks_link_label": {label},
		"thanks_link_url":   {link},
	})
	defer resp.Body.Close()
	return resp, apptest.ReadBody(t, resp)
}

// answerOnce submits the survey at path once, as a new respondent, and
// returns the thanks page.
func answerOnce(t *testing.T, app *apptest.App, path, answer string) string {
	t.Helper()
	respondent := jarClient(t)
	page := mustGet(t, respondent, app.Server.URL+path)
	form := respondForm(t, page)
	form.Set("q_"+extractAnswerFields(t, page)[0], answer)
	resp, body := submitAfterReadingTo(t, app, respondent, path, form)
	if resp.StatusCode != http.StatusOK || !bodyContains(body, "Thank you") {
		t.Fatalf("submit did not succeed (status %d):\n%s", resp.StatusCode, body)
	}
	return body
}

// TestThanksMessage_DefaultWhenEmpty: a survey whose creator wrote no
// message thanks a respondent exactly as before.
func TestThanksMessage_DefaultWhenEmpty(t *testing.T) {
	t.Parallel()
	app := apptest.New(t, apptest.Options{})
	creator := app.Login(t, apptest.UniqueEmail("thanks-default"))
	id := publishedSurvey(t, app, creator, "Default thanks", true,
		[3]string{"short_text", "Anything?", ""},
	)

	body := answerOnce(t, app, "/s/"+id, "Nothing")
	if !bodyContains(body, defaultThanks) {
		t.Errorf("the default text is missing:\n%s", body)
	}
	if strings.Contains(body, thanksMessageHook) || strings.Contains(body, thanksLinkHook) {
		t.Errorf("a survey with no message shows one:\n%s", body)
	}

	// A message saved empty is still the default, and changes nothing a
	// publish could carry.
	if resp, _ := saveThanks(t, app, creator, id, "  ", "", ""); resp.StatusCode != http.StatusOK {
		t.Fatalf("saving an empty thank you page: status %d", resp.StatusCode)
	}
	if editor := app.SurveyPage(t, creator, id); bodyContains(editor, "Publish version 2") {
		t.Errorf("an empty thank you page offers a publish:\n%s", editor)
	}
}

// TestThanksMessage_ShownAsTextWithItsLink: the creator's words are shown
// as they wrote them, line breaks kept and nothing read as markup, and
// the link goes where they said without carrying the survey's address.
func TestThanksMessage_ShownAsTextWithItsLink(t *testing.T) {
	t.Parallel()
	app := apptest.New(t, apptest.Options{})
	creator := app.Login(t, apptest.UniqueEmail("thanks-custom"))
	id := app.CreateSurvey(t, creator, "Custom thanks", true)
	app.AddQuestion(t, creator, id, "short_text", "Anything?", nil)
	resp, page := saveThanks(t, app, creator, id,
		"Thanks for dining with us!\r\nSee you soon.\r\n\r\n<script>alert(1)</script>",
		"Book a <b>table</b>", "https://example.com/book?table=2&time=8")
	if resp.Request.URL.Query().Get("notice") != "thanks" || !bodyContains(page, "Thank you page saved") {
		t.Fatalf("saving did not land on the editor with its notice (%s):\n%s", resp.Request.URL, page)
	}
	// The form shows what was saved.
	if !bodyContains(page, "Book a <b>table</b>") || !bodyContains(page, "https://example.com/book?table=2&time=8") {
		t.Errorf("the editor does not show the saved thank you page:\n%s", page)
	}
	app.Publish(t, creator, id)

	body := answerOnce(t, app, "/s/"+id, "Lovely")
	if !strings.Contains(body, thanksMessageHook) {
		t.Fatalf("the custom message is not shown:\n%s", body)
	}
	if bodyContains(body, defaultThanks) {
		t.Errorf("the default text is shown beside the custom message:\n%s", body)
	}
	if !regexp.MustCompile(`<p>\s*Thanks for dining with us!\s*<br>\s*See you soon.\s*</p>`).MatchString(body) {
		t.Errorf("the line break within a paragraph was not kept:\n%s", body)
	}
	if strings.Contains(body, "<script>alert(1)</script>") || !strings.Contains(body, "&lt;script&gt;alert(1)&lt;/script&gt;") {
		t.Errorf("the message was not escaped:\n%s", body)
	}
	if strings.Contains(body, "<b>table</b>") || !bodyContains(body, "Book a <b>table</b>") {
		t.Errorf("the link label was not escaped:\n%s", body)
	}
	for _, want := range []string{
		`href="https://example.com/book?table=2&amp;time=8"`,
		`rel="noopener noreferrer"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("the link is missing %s:\n%s", want, body)
		}
	}
	// The anonymity reminder is the product's promise, not the creator's
	// text, and stays.
	if !bodyContains(body, "This survey was anonymous") {
		t.Errorf("the anonymity reminder is gone:\n%s", body)
	}
}

// TestThanksMessage_UnrecordedPathsShowItToo: the honeypot and a repeated
// submit look like success, so they carry the message as well, but
// still read back nothing.
func TestThanksMessage_UnrecordedPathsShowItToo(t *testing.T) {
	t.Parallel()
	app := apptest.New(t, apptest.Options{})
	creator := app.Login(t, apptest.UniqueEmail("thanks-unrecorded"))
	id := app.CreateSurvey(t, creator, "Unrecorded thanks", true)
	app.AddQuestion(t, creator, id, "short_text", "Say something", nil)
	saveThanks(t, app, creator, id, "Cheers from the kitchen", "Menu", "https://example.com/menu")
	app.Publish(t, creator, id)

	bot := &http.Client{}
	page := mustGet(t, bot, app.Server.URL+"/s/"+id)
	form := respondForm(t, page)
	form.Set("q_"+extractAnswerFields(t, page)[0], "echo me back")
	form.Set("website", "https://spam.example")
	_, body := submitAfterReading(t, app, bot, id, form)
	if !bodyContains(body, "Cheers from the kitchen") || !strings.Contains(body, thanksLinkHook) {
		t.Errorf("the honeypot's thanks differs from a person's:\n%s", body)
	}
	if strings.Contains(body, summaryHook) || bodyContains(body, "echo me back") {
		t.Errorf("the honeypot echoed what was sent:\n%s", body)
	}

	person := &http.Client{}
	page = mustGet(t, person, app.Server.URL+"/s/"+id)
	form = respondForm(t, page)
	form.Set("q_"+extractAnswerFields(t, page)[0], "said once")
	submitAfterReading(t, app, person, id, form)
	_, second := submitAfterReading(t, app, person, id, form)
	if !bodyContains(second, "Cheers from the kitchen") {
		t.Errorf("a repeated submit lost the message:\n%s", second)
	}
	if strings.Contains(second, summaryHook) {
		t.Errorf("a repeated submit read back answers it did not record:\n%s", second)
	}
}

// TestThanksMessage_InvitedParticipant: the personal link's thanks page
// carries the message as the public one does.
func TestThanksMessage_InvitedParticipant(t *testing.T) {
	t.Parallel()
	app := apptest.New(t, apptest.Options{})
	creator := app.Login(t, apptest.UniqueEmail("thanks-invite-msg"))
	id := app.CreateSurvey(t, creator, "Invited thanks", false)
	app.AddQuestion(t, creator, id, "short_text", "Your team?", nil)
	saveThanks(t, app, creator, id, "Thanks, team", "", "")
	app.Publish(t, creator, id)
	addr := apptest.UniqueEmail("thanks-invitee")
	app.PostForm(t, creator, "/surveys/"+id+"/participants", url.Values{"emails": {addr}}).Body.Close()
	sendInvites(t, app, creator, id)
	link := inviteLinkTo(t, app, addr)

	body := answerOnce(t, app, link[strings.Index(link, "/p/"):], "Platform")
	if !bodyContains(body, "Thanks, team") || bodyContains(body, defaultThanks) {
		t.Errorf("the participant's thanks page lacks the message:\n%s", body)
	}
	if strings.Contains(body, thanksLinkHook) {
		t.Errorf("a message with no link shows one:\n%s", body)
	}
}

// TestThanksMessage_FrozenPerVersion is ADR-0001 for the thank you page:
// editing it changes the draft, the live page keeps the published one
// until the next publish, and a change to it alone is worth publishing.
func TestThanksMessage_FrozenPerVersion(t *testing.T) {
	t.Parallel()
	app := apptest.New(t, apptest.Options{})
	creator := app.Login(t, apptest.UniqueEmail("thanks-frozen"))
	id := app.CreateSurvey(t, creator, "Frozen thanks", true)
	app.AddQuestion(t, creator, id, "short_text", "Anything?", nil)
	saveThanks(t, app, creator, id, "First message", "", "")
	app.Publish(t, creator, id)

	_, editor := saveThanks(t, app, creator, id, "Second message", "Site", "https://example.com")
	if !bodyContains(editor, "Publish version 2") {
		t.Fatalf("a changed thank you page does not offer a publish:\n%s", editor)
	}
	live := answerOnce(t, app, "/s/"+id, "before")
	if !bodyContains(live, "First message") || bodyContains(live, "Second message") || strings.Contains(live, thanksLinkHook) {
		t.Fatalf("the draft reached respondents before it was published:\n%s", live)
	}

	if body := app.Publish(t, creator, id); !bodyContains(body, "Published version 2") {
		t.Fatalf("publishing the thank you page was refused:\n%s", body)
	}
	live = answerOnce(t, app, "/s/"+id, "after")
	if !bodyContains(live, "Second message") || !strings.Contains(live, thanksLinkHook) {
		t.Errorf("the published thank you page is not shown:\n%s", live)
	}

	// Clearing it is a change too, back to the default.
	saveThanks(t, app, creator, id, "", "", "")
	if body := app.Publish(t, creator, id); !bodyContains(body, "Published version 3") {
		t.Fatalf("publishing the cleared thank you page was refused:\n%s", body)
	}
	if live = answerOnce(t, app, "/s/"+id, "cleared"); !bodyContains(live, defaultThanks) {
		t.Errorf("a cleared message does not restore the default:\n%s", live)
	}
}

// TestThanksMessage_RefusesUnsafeLinks: the editor refuses an address
// that is not a web page, says why, keeps what was typed, and leaves the
// draft as it was.
func TestThanksMessage_RefusesUnsafeLinks(t *testing.T) {
	t.Parallel()
	app := apptest.New(t, apptest.Options{})
	creator := app.Login(t, apptest.UniqueEmail("thanks-unsafe"))
	id := app.CreateSurvey(t, creator, "Unsafe link", true)
	app.AddQuestion(t, creator, id, "short_text", "Anything?", nil)

	for _, link := range []string{"javascript:alert(1)", "/surveys", "ftp://example.com/file"} {
		resp, body := saveThanks(t, app, creator, id, "Kept message", "Go", link)
		if resp.StatusCode != http.StatusUnprocessableEntity {
			t.Errorf("%s: status %d, want 422", link, resp.StatusCode)
		}
		if !bodyContains(body, "the link address must start with http:// or https://") {
			t.Errorf("%s: the refusal is not explained:\n%s", link, body)
		}
		if !bodyContains(body, "Kept message") || !bodyContains(body, link) {
			t.Errorf("%s: what was typed was lost:\n%s", link, body)
		}
	}
	_, body := saveThanks(t, app, creator, id, "Kept message", "", "https://example.com")
	if !bodyContains(body, "give the link a label") {
		t.Errorf("an unlabelled link is not refused:\n%s", body)
	}
	if editor := app.SurveyPage(t, creator, id); bodyContains(editor, "Kept message") {
		t.Errorf("a refused thank you page was saved:\n%s", editor)
	}
}

// TestThanksMessage_Localized: the message and link label are translated
// with the questions, a language is not ready until they are, and a
// respondent reading Dutch is thanked in Dutch.
func TestThanksMessage_Localized(t *testing.T) {
	t.Parallel()
	app := apptest.New(t, apptest.Options{AI: translatorFake()})
	creator := app.Login(t, apptest.UniqueEmail("thanks-lang-msg"))
	id := app.CreateSurvey(t, creator, "Localized thanks", true)
	app.AddQuestion(t, creator, id, "short_text", "Why?", nil)
	identity := app.QuestionIdentities(t, creator, id)[0]
	saveThanks(t, app, creator, id, "Thank you!", "Book a table", "https://example.com/book")
	app.PostForm(t, creator, "/surveys/"+id+"/localizations", url.Values{"lang": {"nl"}}).Body.Close()
	app.PostForm(t, creator, "/surveys/"+id+"/localizations/nl", url.Values{
		"t_" + identity: {"Waarom?"},
	}).Body.Close()

	if body := app.Publish(t, creator, id); bodyContains(body, "Published version 1") {
		t.Fatalf("a language without its thank you page was published:\n%s", body)
	}

	// The model drafts it; a draft is not a review.
	resp := app.PostForm(t, creator, "/surveys/"+id+"/localizations/nl/draft", nil)
	drafted := apptest.ReadBody(t, resp)
	resp.Body.Close()
	if !strings.Contains(drafted, "js-thanks-translation") || !bodyContains(drafted, "[vertaald] de vraag") {
		t.Fatalf("the thank you page was not drafted:\n%s", drafted)
	}
	if body := app.Publish(t, creator, id); bodyContains(body, "Published version 1") {
		t.Fatalf("an unreviewed thank you page was published:\n%s", body)
	}

	app.PostForm(t, creator, "/surveys/"+id+"/localizations/nl", url.Values{
		"t_" + identity:     {"Waarom?"},
		"thanks_message":    {"Bedankt!"},
		"thanks_link_label": {"Reserveer een tafel"},
	}).Body.Close()
	if body := app.Publish(t, creator, id); !bodyContains(body, "Published version 1") {
		t.Fatalf("a reviewed language was refused:\n%s", body)
	}

	dutch := answerOnce(t, app, "/s/"+id+"?lang=nl", "Omdat")
	if !bodyContains(dutch, "Bedankt!") || !bodyContains(dutch, "Reserveer een tafel") {
		t.Errorf("the Dutch respondent was not thanked in Dutch:\n%s", dutch)
	}
	if bodyContains(dutch, "Thank you!") || !strings.Contains(dutch, `href="https://example.com/book"`) {
		t.Errorf("the Dutch thanks page shows the original words or lost the link:\n%s", dutch)
	}
	original := answerOnce(t, app, "/s/"+id, "Because")
	if !bodyContains(original, "Thank you!") || !bodyContains(original, "Book a table") {
		t.Errorf("the original thanks page lost the creator's words:\n%s", original)
	}

	// Rewording the message sends the translation back for review.
	saveThanks(t, app, creator, id, "Thank you so much!", "Book a table", "https://example.com/book")
	languages := mustGet(t, creator, app.Server.URL+"/surveys/"+id+"/localizations")
	if !bodyContains(languages, "The thank you page changed after this was translated") {
		t.Errorf("a stale translation is not marked:\n%s", languages)
	}
	if body := app.Publish(t, creator, id); bodyContains(body, "Published version 2") {
		t.Errorf("a stale thank you translation was published:\n%s", body)
	}
}

// TestThanksMessage_Exported: the workspace export carries each version's
// thank you page, and omits it where the default was shown.
func TestThanksMessage_Exported(t *testing.T) {
	t.Parallel()
	app := apptest.New(t, apptest.Options{})
	creator := app.Login(t, apptest.UniqueEmail("thanks-export"))
	id := app.CreateSurvey(t, creator, "Exported thanks", true)
	app.AddQuestion(t, creator, id, "short_text", "Anything?", nil)
	app.Publish(t, creator, id)
	saveThanks(t, app, creator, id, "Thanks\nagain", "Menu", "https://example.com/menu")
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
			switch version.Number {
			case 1:
				if version.Thanks != nil {
					t.Errorf("version 1 showed the default, exported %+v", version.Thanks)
				}
			case 2:
				want := export.Thanks{Message: "Thanks\nagain", LinkLabel: "Menu", LinkURL: "https://example.com/menu"}
				if version.Thanks == nil || *version.Thanks != want {
					t.Errorf("version 2 thanks = %+v, want %+v", version.Thanks, want)
				}
			}
		}
		return
	}
	t.Fatalf("the survey is missing from the export")
}
