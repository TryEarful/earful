package http_test

import (
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/TryEarful/earful/internal/ai"
	"github.com/TryEarful/earful/internal/apptest"
)

// translatorFake answers every Translate call with a marked string, so a
// test can tell a translation from an original at a glance.
func translatorFake() *ai.Fake {
	return &ai.Fake{TranslateScript: [][]string{{"[vertaald] ", "de vraag"}}}
}

// TestLocalization_UnreviewedTranslationsCannotBePublished is story 23,
// and the reason this feature has a review step at all: a machine
// translation goes out in the creator's name, in a language they may not
// read.
func TestLocalization_UnreviewedTranslationsCannotBePublished(t *testing.T) {
	t.Parallel()
	fake := translatorFake()
	app := apptest.New(t, apptest.Options{AI: fake})
	creator := app.Login(t, apptest.UniqueEmail("localize"))

	id := app.CreateSurvey(t, creator, "Localized survey", true)
	app.AddQuestion(t, creator, id, "long_text", "How was your first week?", nil)

	// Add a language and let the model draft it.
	app.PostForm(t, creator, "/surveys/"+id+"/localizations", url.Values{"lang": {"nl"}}).Body.Close()
	resp := app.PostForm(t, creator, "/surveys/"+id+"/localizations/nl/draft", nil)
	page := apptest.ReadBody(t, resp)
	resp.Body.Close()

	if !bodyContains(page, "[vertaald] de vraag") {
		t.Fatalf("the drafted translation is not on the page:\n%s", page)
	}
	if !bodyContains(page, "not yet reviewed") {
		t.Errorf("a drafted translation is not marked unreviewed:\n%s", page)
	}
	if len(fake.TranslateCalls) != 1 {
		t.Fatalf("model calls = %d, want one per question", len(fake.TranslateCalls))
	}
	if fake.TranslateCalls[0].TargetLang != "Dutch" {
		t.Errorf("target language = %q", fake.TranslateCalls[0].TargetLang)
	}

	// Publishing is refused while anything is unreviewed.
	body := app.Publish(t, creator, id)
	if !bodyContains(body, "review every translation before publishing") {
		t.Errorf("publish was allowed with an unreviewed translation:\n%s", body)
	}

	// Review it — that is what the save does — and publishing works.
	identity := app.QuestionIdentities(t, creator, id)[0]
	app.PostForm(t, creator, "/surveys/"+id+"/localizations/nl", url.Values{
		"t_" + identity: {"Hoe was je eerste week?"},
	}).Body.Close()

	body = app.Publish(t, creator, id)
	if !bodyContains(body, "Published version 1") {
		t.Fatalf("publish still refused after review:\n%s", body)
	}

	// The respondent can now answer in Dutch, and the frozen wording is
	// the reviewed one.
	respondent := &http.Client{}
	original := mustGet(t, respondent, app.Server.URL+"/s/"+id)
	if !bodyContains(original, "How was your first week?") {
		t.Errorf("the original language is not served by default:\n%s", original)
	}
	dutch := mustGet(t, respondent, app.Server.URL+"/s/"+id+"?lang=nl")
	if !bodyContains(dutch, "Hoe was je eerste week?") {
		t.Errorf("the Dutch version is not served:\n%s", dutch)
	}
	if !strings.Contains(dutch, `<html lang="nl">`) {
		t.Errorf("the page does not declare its language:\n%s", dutch[:200])
	}
}

// TestLocalization_RewordingUnreviewsTheTranslation: a translation of a
// wording the creator has since changed has not been reviewed, whatever
// it was marked before.
func TestLocalization_RewordingUnreviewsTheTranslation(t *testing.T) {
	t.Parallel()
	app := apptest.New(t, apptest.Options{AI: translatorFake()})
	creator := app.Login(t, apptest.UniqueEmail("reword-localize"))

	id := app.CreateSurvey(t, creator, "Reworded", true)
	app.AddQuestion(t, creator, id, "short_text", "How was it?", nil)
	identity := app.QuestionIdentities(t, creator, id)[0]

	app.PostForm(t, creator, "/surveys/"+id+"/localizations", url.Values{"lang": {"nl"}}).Body.Close()
	app.PostForm(t, creator, "/surveys/"+id+"/localizations/nl", url.Values{
		"t_" + identity: {"Hoe was het?"},
	}).Body.Close()
	if body := app.Publish(t, creator, id); !bodyContains(body, "Published version 1") {
		t.Fatalf("publish refused with a reviewed translation:\n%s", body)
	}

	// Change the question; the translation is now of an old wording.
	app.PostForm(t, creator, "/surveys/"+id+"/questions/"+identity, url.Values{
		"type": {"short_text"}, "text": {"Looking back, how was it?"},
	}).Body.Close()

	page := mustGet(t, creator, app.Server.URL+"/surveys/"+id+"/localizations")
	if !bodyContains(page, "The question changed after this was translated") {
		t.Errorf("a stale translation is not flagged:\n%s", page)
	}
	if body := app.Publish(t, creator, id); !bodyContains(body, "review every translation") {
		t.Errorf("publish was allowed with a stale translation:\n%s", body)
	}
}

// TestLocalization_RespondentChoiceIsStoredNowhere is story 25: the
// language a respondent picks is a fact about them, and Earful keeps it
// for exactly as long as the request.
func TestLocalization_RespondentChoiceIsStoredNowhere(t *testing.T) {
	t.Parallel()
	app := apptest.New(t, apptest.Options{AI: translatorFake()})
	creator := app.Login(t, apptest.UniqueEmail("langchoice"))

	id := app.CreateSurvey(t, creator, "Language choice", true)
	app.AddQuestion(t, creator, id, "short_text", "How was it?", nil)
	identity := app.QuestionIdentities(t, creator, id)[0]
	app.PostForm(t, creator, "/surveys/"+id+"/localizations", url.Values{"lang": {"nl"}}).Body.Close()
	app.PostForm(t, creator, "/surveys/"+id+"/localizations/nl", url.Values{
		"t_" + identity: {"Hoe was het?"},
	}).Body.Close()
	app.Publish(t, creator, id)

	respondent := &http.Client{}
	req, err := http.NewRequest(http.MethodGet, app.Server.URL+"/s/"+id+"?lang=nl", nil)
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	req.Header.Set("Accept-Language", "nl-BE,nl;q=0.9,en;q=0.8")
	resp, err := respondent.Do(req)
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()
	body := apptest.ReadBody(t, resp)

	if !bodyContains(body, "Hoe was het?") {
		t.Fatalf("the chosen language was not served:\n%s", body)
	}
	// No cookie remembers the choice.
	for _, cookie := range resp.Cookies() {
		if strings.Contains(strings.ToLower(cookie.Name), "lang") {
			t.Errorf("the language choice was stored in a cookie: %s", cookie.Name)
		}
	}
	// And the picker suggests the browser's language without having
	// chosen it for them.
	req, err = http.NewRequest(http.MethodGet, app.Server.URL+"/s/"+id, nil)
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	req.Header.Set("Accept-Language", "nl-BE,nl;q=0.9,en;q=0.8")
	suggested, err := respondent.Do(req)
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer suggested.Body.Close()
	fresh := apptest.ReadBody(t, suggested)
	if !bodyContains(fresh, "suggested") {
		t.Errorf("the browser's language is not suggested:\n%s", fresh)
	}
	if !bodyContains(fresh, "How was it?") {
		t.Error("a suggestion was applied as a choice")
	}
}

// TestAnswerTranslation_KeepsTheOriginal is stories 26 and 27: a creator
// can read a global audience without the product ever replacing what
// somebody actually said.
func TestAnswerTranslation_KeepsTheOriginal(t *testing.T) {
	t.Parallel()
	fake := &ai.Fake{TranslateScript: [][]string{{"It was excellent"}}}
	app := apptest.New(t, apptest.Options{AI: fake})
	creator := app.Login(t, apptest.UniqueEmail("answer-translate"))

	id := app.CreateSurvey(t, creator, "Global survey", true)
	app.AddQuestion(t, creator, id, "long_text", "How was it?", nil)
	app.Publish(t, creator, id)
	answerSurvey(t, app, id, map[int]string{0: "Het was uitstekend"})

	resp := app.PostForm(t, creator, "/surveys/"+id+"/answers/translate", url.Values{"lang": {"en"}})
	resp.Body.Close()

	page := mustGet(t, creator, app.Server.URL+"/surveys/"+id+"/results?lang=en")
	if !bodyContains(page, "Het was uitstekend") {
		t.Errorf("the original answer is gone:\n%s", page)
	}
	if !bodyContains(page, "It was excellent") {
		t.Errorf("the translation is not shown:\n%s", page)
	}
	if !bodyContains(page, "Machine translation") {
		t.Errorf("the translation is not marked as machine-made:\n%s", page)
	}

	// Translating again uses the cache rather than the model.
	before := len(fake.TranslateCalls)
	app.PostForm(t, creator, "/surveys/"+id+"/answers/translate", url.Values{"lang": {"en"}}).Body.Close()
	if len(fake.TranslateCalls) != before {
		t.Errorf("a second translation call was made for an already-translated answer")
	}

	// The export carries what the respondent said, untouched.
	csv := mustGet(t, creator, app.Server.URL+"/surveys/"+id+"/results.csv")
	if !strings.Contains(csv, "Het was uitstekend") {
		t.Errorf("the export lost the original answer:\n%s", csv)
	}
}

// TestVoice_UsesTheChosenLanguage is M11-T3: the language a respondent
// picked drives transcription, so their words come back as their words.
func TestVoice_UsesTheChosenLanguage(t *testing.T) {
	t.Parallel()
	fake := &ai.Fake{
		TranslateScript:  [][]string{{"Hoe was het?"}},
		TranscribeScript: [][]string{{"het was prima"}},
	}
	app := apptest.New(t, apptest.Options{AI: fake})
	creator := app.Login(t, apptest.UniqueEmail("voice-lang"))

	id := app.CreateSurvey(t, creator, "Spoken Dutch", true)
	app.AddQuestion(t, creator, id, "long_text", "How was it?", nil)
	identity := app.QuestionIdentities(t, creator, id)[0]
	app.PostForm(t, creator, "/surveys/"+id+"/localizations", url.Values{"lang": {"nl"}}).Body.Close()
	app.PostForm(t, creator, "/surveys/"+id+"/localizations/nl", url.Values{
		"t_" + identity: {"Hoe was het?"},
	}).Body.Close()
	app.Publish(t, creator, id)

	respondent := &http.Client{}
	page := mustGet(t, respondent, app.Server.URL+"/s/"+id+"?lang=nl")

	conn := dialVoice(t, app, "/s/"+id+"/voice", page)
	speak(t, conn, 1)
	transcript, frame := readVoice(t, conn)
	if frame.Type != "done" || transcript != "het was prima" {
		t.Fatalf("transcript = %q, last frame %+v", transcript, frame)
	}
	if len(fake.TranscribeCalls) != 1 {
		t.Fatalf("transcribe calls = %d", len(fake.TranscribeCalls))
	}
	// dialVoice sends what the page declares, which is now Dutch.
	if fake.TranscribeCalls[0].Language != "nl" {
		t.Errorf("transcription language = %q, want nl", fake.TranscribeCalls[0].Language)
	}
}
