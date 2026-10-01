package http_test

import (
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"strings"
	"testing"

	"github.com/TryEarful/earful/internal/apptest"
)

const summaryHook = "js-answer-summary"

func jarClient(t *testing.T) *http.Client {
	t.Helper()
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatalf("cookie jar: %v", err)
	}
	return &http.Client{Jar: jar}
}

// TestThanks_ReadsBackTheAnswers: after submitting, a respondent sees
// what they sent, question by question, and a skipped question says so.
func TestThanks_ReadsBackTheAnswers(t *testing.T) {
	t.Parallel()
	app := apptest.New(t, apptest.Options{})
	creator := app.Login(t, apptest.UniqueEmail("thanks-summary"))
	id := publishedSurvey(t, app, creator, "Readback", true,
		[3]string{"long_text", "What would you change?", ""},
		[3]string{"single_choice", "How often?", "Weekly\nMonthly"},
		[3]string{"yes_no", "Would you come back?", ""},
		[3]string{"short_text", "Anything else?", ""},
	)

	respondent := jarClient(t)
	page := mustGet(t, respondent, app.Server.URL+"/s/"+id)
	fields := extractAnswerFields(t, page)
	form := respondForm(t, page)
	form.Set("q_"+fields[0], "Shorter sessions\nand more breaks")
	form.Set("q_"+fields[1], "Monthly")
	form.Set("q_"+fields[2], "yes")
	resp, body := submitAfterReading(t, app, respondent, id, form)
	if resp.StatusCode != http.StatusOK || !bodyContains(body, "Thank you") {
		t.Fatalf("submit did not succeed (status %d):\n%s", resp.StatusCode, body)
	}
	if !strings.Contains(body, summaryHook) || !bodyContains(body, "Your answers") {
		t.Fatalf("the thanks page carries no summary:\n%s", body)
	}
	for _, want := range []string{
		"What would you change?", "Shorter sessions\nand more breaks",
		"How often?", "Monthly",
		"Would you come back?", "Yes",
		"Anything else?", "No answer",
	} {
		if !bodyContains(body, want) {
			t.Errorf("the summary is missing %q:\n%s", want, body)
		}
	}
	// The page is the POST response and nothing else: never stored by the
	// browser, and no address leads back to it.
	if got := resp.Header.Get("Cache-Control"); got != "no-store" {
		t.Errorf("Cache-Control = %q, want no-store", got)
	}
	if resp.Request.Method != http.MethodPost || resp.Request.URL.Path != "/s/"+id {
		t.Errorf("the thanks page was reached by %s %s, want the POST itself", resp.Request.Method, resp.Request.URL.Path)
	}
	for _, path := range []string{"/s/" + id, "/s/" + id + "/thanks"} {
		again := mustGet(t, respondent, app.Server.URL+path)
		if strings.Contains(again, summaryHook) || bodyContains(again, "Shorter sessions") {
			t.Errorf("GET %s shows the answers again:\n%s", path, again)
		}
	}
}

// TestThanks_InTheLanguageAnswered: the summary repeats the question and
// the choice as the respondent read them, not as the creator wrote them,
// and the page declares that language.
func TestThanks_InTheLanguageAnswered(t *testing.T) {
	t.Parallel()
	app := apptest.New(t, apptest.Options{AI: translatorFake()})
	creator := app.Login(t, apptest.UniqueEmail("thanks-lang"))
	id := dutchChoiceSurvey(t, app, creator, "Dutch readback", true)

	respondent := &http.Client{}
	page := mustGet(t, respondent, app.Server.URL+"/s/"+id+"?lang=nl")
	fields := extractAnswerFields(t, page)
	form := respondForm(t, page)
	form.Set("q_"+fields[0], "Maandelijks")
	form.Set("q_"+fields[1], "Omdat het kan")
	_, thanks := submitAfterReadingTo(t, app, respondent, "/s/"+id+"?lang=nl", form)
	for _, want := range []string{"Hoe vaak?", "Maandelijks", "Waarom?", "Omdat het kan"} {
		if !bodyContains(thanks, want) {
			t.Errorf("the summary is missing %q:\n%s", want, thanks)
		}
	}
	if bodyContains(thanks, "Monthly") || bodyContains(thanks, "How often?") {
		t.Errorf("the summary is in the creator's wording:\n%s", thanks)
	}
	if !strings.Contains(thanks, `<html lang="nl">`) {
		t.Errorf("the thanks page does not declare the language answered in:\n%s", thanks[:200])
	}
}

// TestThanks_InvitedReadsBackOnce: a participant sees their answers once,
// and their personal link does not show them again.
func TestThanks_InvitedReadsBackOnce(t *testing.T) {
	t.Parallel()
	app := apptest.New(t, apptest.Options{})
	creator := app.Login(t, apptest.UniqueEmail("thanks-invited"))
	id := publishedSurvey(t, app, creator, "Invited readback", false,
		[3]string{"short_text", "Your team?", ""},
	)
	addr := apptest.UniqueEmail("thanks-participant")
	app.PostForm(t, creator, "/surveys/"+id+"/participants", url.Values{"emails": {addr}}).Body.Close()
	sendInvites(t, app, creator, id)
	link := inviteLinkTo(t, app, addr)
	path := link[strings.Index(link, "/p/"):]

	participant := jarClient(t)
	page := mustGet(t, participant, link)
	form := respondForm(t, page)
	form.Set("q_"+extractAnswerFields(t, page)[0], "Platform")
	resp, thanks := submitAfterReadingTo(t, app, participant, path, form)
	if resp.StatusCode != http.StatusOK || !strings.Contains(thanks, summaryHook) {
		t.Fatalf("the participant's thanks page carries no summary (status %d):\n%s", resp.StatusCode, thanks)
	}
	if !bodyContains(thanks, "Your team?") || !bodyContains(thanks, "Platform") {
		t.Errorf("the summary is missing the answer:\n%s", thanks)
	}
	if got := resp.Header.Get("Cache-Control"); got != "no-store" {
		t.Errorf("Cache-Control = %q, want no-store", got)
	}
	again := mustGet(t, participant, link)
	if strings.Contains(again, summaryHook) || bodyContains(again, "Platform") {
		t.Errorf("the personal link shows the answers again:\n%s", again)
	}
}

// TestThanks_NothingRecordedNothingEchoed: the honeypot and a repeated
// submit look like success but read nothing back, since nothing was
// recorded from either request.
func TestThanks_NothingRecordedNothingEchoed(t *testing.T) {
	t.Parallel()
	app := apptest.New(t, apptest.Options{})
	creator := app.Login(t, apptest.UniqueEmail("thanks-echo"))
	id := publishedSurvey(t, app, creator, "No echo", true,
		[3]string{"short_text", "Say something", ""},
	)

	bot := &http.Client{}
	page := mustGet(t, bot, app.Server.URL+"/s/"+id)
	form := respondForm(t, page)
	form.Set("q_"+extractAnswerFields(t, page)[0], "echo me back")
	form.Set("website", "https://spam.example")
	_, body := submitAfterReading(t, app, bot, id, form)
	if !bodyContains(body, "Thank you") {
		t.Fatalf("the honeypot should look like success:\n%s", body)
	}
	if strings.Contains(body, summaryHook) || bodyContains(body, "echo me back") {
		t.Errorf("the honeypot echoed what was sent:\n%s", body)
	}

	person := &http.Client{}
	page = mustGet(t, person, app.Server.URL+"/s/"+id)
	form = respondForm(t, page)
	form.Set("q_"+extractAnswerFields(t, page)[0], "said once")
	if _, first := submitAfterReading(t, app, person, id, form); !strings.Contains(first, summaryHook) {
		t.Fatalf("the first submit carries no summary:\n%s", first)
	}
	_, second := submitAfterReading(t, app, person, id, form)
	if !bodyContains(second, "Thank you") {
		t.Fatalf("a repeated submit should be thanked:\n%s", second)
	}
	if strings.Contains(second, summaryHook) || bodyContains(second, "said once") {
		t.Errorf("a repeated submit read back answers it did not record:\n%s", second)
	}
}

// TestPreview_ReadsBackTheAnswers: a creator submitting the preview sees
// the same summary a respondent would, and still nothing is recorded.
func TestPreview_ReadsBackTheAnswers(t *testing.T) {
	t.Parallel()
	app := apptest.New(t, apptest.Options{})
	creator := app.Login(t, apptest.UniqueEmail("preview-summary"))
	id := app.CreateSurvey(t, creator, "Preview readback", true)
	app.AddQuestion(t, creator, id, "yes_no", "Is this clear?", nil)
	app.AddQuestion(t, creator, id, "long_text", "What is missing?", nil)

	page := getBody(t, creator, app.Server.URL+"/surveys/"+id+"/preview")
	fields := extractAnswerFields(t, page)
	resp := app.PostForm(t, creator, "/surveys/"+id+"/preview", url.Values{
		"q_" + fields[0]: {"no"},
	})
	body := apptest.ReadBody(t, resp)
	resp.Body.Close()
	if !bodyContains(body, "Nothing was submitted") || !strings.Contains(body, summaryHook) {
		t.Fatalf("the preview submit carries no summary:\n%s", body)
	}
	for _, want := range []string{"Is this clear?", "No", "What is missing?", "No answer"} {
		if !bodyContains(body, want) {
			t.Errorf("the preview summary is missing %q:\n%s", want, body)
		}
	}
	if editor := app.SurveyPage(t, creator, id); !bodyContains(editor, "Not published yet") {
		t.Errorf("preview appears to have published or altered the survey:\n%s", editor)
	}
}
