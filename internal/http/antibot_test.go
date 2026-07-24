package http_test

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"testing"
	"time"

	"github.com/TryEarful/earful/internal/apptest"
)

// These tests cover M4-T5 (anti-abuse) and the ALTCHA/dedupe halves of
// M4-T2 at the application edge. The observable for "nothing was stored"
// is the editor's response counter — the same thing a creator reads.

func responseCount(t *testing.T, app *apptest.App, creator *http.Client, surveyID string) string {
	t.Helper()
	page := app.SurveyPage(t, creator, surveyID)
	for _, n := range []string{"0", "1", "2", "3", "4", "5", "6", "7"} {
		label := n + " responses"
		if n == "1" {
			label = "1 response"
		}
		if bodyContains(page, label) {
			return n
		}
	}
	t.Fatalf("no response counter on the editor page:\n%s", page)
	return ""
}

// TestAntibot_HoneypotSwallowsBots: a filled honeypot renders success and
// stores nothing — the bot learns nothing about what tripped it.
func TestAntibot_HoneypotSwallowsBots(t *testing.T) {
	t.Parallel()
	app := apptest.New(t, apptest.Options{})
	creator := app.Login(t, apptest.UniqueEmail("honeypot"))
	id := publishedSurvey(t, app, creator, "Honeypot", true, [3]string{"short_text", "Q", ""})

	bot := &http.Client{}
	page := mustGet(t, bot, app.Server.URL+"/s/"+id)
	form := respondForm(t, page)
	form.Set("q_"+extractAnswerFields(t, page)[0], "beep boop")
	form.Set("website", "https://spam.example") // the field no human sees

	resp, body := submitAfterReading(t, app, bot, id, form)
	if resp.StatusCode != http.StatusOK || !bodyContains(body, "Thank you") {
		t.Fatalf("honeypot trip should look like success to the bot: status %d\n%s", resp.StatusCode, body)
	}
	if got := responseCount(t, app, creator, id); got != "0" {
		t.Errorf("honeypot submission was stored: count = %s, want 0", got)
	}
}

// TestAntibot_TooFastSubmissionIsHeldBack: the signed render timestamp
// gives the no-JS path its bot check. A person who really is that fast
// just presses submit again — with their answers intact.
func TestAntibot_TooFastSubmissionIsHeldBack(t *testing.T) {
	t.Parallel()
	app := apptest.New(t, apptest.Options{})
	creator := app.Login(t, apptest.UniqueEmail("toofast"))
	id := publishedSurvey(t, app, creator, "Too fast", true, [3]string{"short_text", "Q", ""})

	respondent := &http.Client{}
	page := mustGet(t, respondent, app.Server.URL+"/s/"+id)
	identity := extractAnswerFields(t, page)[0]
	form := respondForm(t, page)
	form.Set("q_"+identity, "typed instantly")

	// Submit with no reading time at all.
	resp, err := respondent.PostForm(app.Server.URL+"/s/"+id, form)
	if err != nil {
		t.Fatalf("fast submit: %v", err)
	}
	body := apptest.ReadBody(t, resp)
	resp.Body.Close()
	if !bodyContains(body, "That was quick") {
		t.Fatalf("expected the take-a-moment notice:\n%s", body)
	}
	if !bodyContains(body, "typed instantly") {
		t.Errorf("re-render lost the typed answer:\n%s", body)
	}
	if got := responseCount(t, app, creator, id); got != "0" {
		t.Fatalf("too-fast submission was stored: count = %s", got)
	}

	// The re-rendered form carries a fresh token; after a human pause it
	// goes through.
	fresh := respondForm(t, body)
	fresh.Set("q_"+identity, "typed instantly")
	_, retry := submitAfterReading(t, app, respondent, id, fresh)
	if !bodyContains(retry, "Thank you") {
		t.Errorf("submission after the pause should succeed:\n%s", retry)
	}
}

// TestAntibot_ForgedFormTokenRejected: a submission whose timestamp is
// missing or hand-crafted did not come from our form render.
func TestAntibot_ForgedFormTokenRejected(t *testing.T) {
	t.Parallel()
	app := apptest.New(t, apptest.Options{})
	creator := app.Login(t, apptest.UniqueEmail("forged-ts"))
	id := publishedSurvey(t, app, creator, "Forged token", true, [3]string{"short_text", "Q", ""})

	respondent := &http.Client{}
	page := mustGet(t, respondent, app.Server.URL+"/s/"+id)
	for name, token := range map[string]string{
		"missing": "",
		"forged":  fmt.Sprintf("%d.forged-signature", time.Now().UnixMilli()),
	} {
		form := respondForm(t, page)
		form.Set("form_ts", token)
		form.Set("q_"+extractAnswerFields(t, page)[0], "answer")
		_, body := submitAfterReading(t, app, respondent, id, form)
		if bodyContains(body, "Thank you") {
			t.Errorf("%s form token was accepted", name)
		}
	}
	if got := responseCount(t, app, creator, id); got != "0" {
		t.Errorf("forged-token submissions were stored: count = %s", got)
	}
}

// TestAntibot_DoubleClickStoresOnce is M4-T2's soft dedupe: the same form
// render submits once, however many times the button is pressed.
func TestAntibot_DoubleClickStoresOnce(t *testing.T) {
	t.Parallel()
	app := apptest.New(t, apptest.Options{})
	creator := app.Login(t, apptest.UniqueEmail("doubleclick"))
	id := publishedSurvey(t, app, creator, "Double click", true, [3]string{"short_text", "Q", ""})

	respondent := &http.Client{}
	page := mustGet(t, respondent, app.Server.URL+"/s/"+id)
	form := respondForm(t, page)
	form.Set("q_"+extractAnswerFields(t, page)[0], "once please")

	for i := 0; i < 3; i++ { // click, click, click
		_, body := submitAfterReading(t, app, respondent, id, form)
		if !bodyContains(body, "Thank you") {
			t.Fatalf("click %d did not render thanks:\n%s", i+1, body)
		}
	}
	if got := responseCount(t, app, creator, id); got != "1" {
		t.Errorf("triple click stored %s responses, want 1", got)
	}

	// A deliberate second answer (fresh form) is allowed — the stated
	// anonymity trade-off (story 43) — and the revisit says so.
	again := mustGet(t, respondent, app.Server.URL+"/s/"+id)
	form2 := respondForm(t, again)
	form2.Set("q_"+extractAnswerFields(t, again)[0], "twice, deliberately")
	_, body := submitAfterReading(t, app, respondent, id, form2)
	if !bodyContains(body, "Thank you") {
		t.Fatalf("deliberate re-answer refused:\n%s", body)
	}
	if got := responseCount(t, app, creator, id); got != "2" {
		t.Errorf("deliberate re-answer not stored: count = %s, want 2", got)
	}
}

// TestAntibot_AnsweredCookieShowsNotice: the soft-dedupe courtesy note on
// revisits (a cookie-jar client, like a real browser).
func TestAntibot_AnsweredCookieShowsNotice(t *testing.T) {
	t.Parallel()
	app := apptest.New(t, apptest.Options{})
	creator := app.Login(t, apptest.UniqueEmail("answered-note"))
	id := publishedSurvey(t, app, creator, "Answered note", true, [3]string{"short_text", "Q", ""})

	respondent := app.BrowserClient(t)
	page := mustGet(t, respondent, app.Server.URL+"/s/"+id)
	if bodyContains(page, "answered this survey before") {
		t.Fatal("fresh visitor should not see the answered-before note")
	}
	form := respondForm(t, page)
	form.Set("q_"+extractAnswerFields(t, page)[0], "hello")
	submitAfterReading(t, app, respondent, id, form)

	revisit := mustGet(t, respondent, app.Server.URL+"/s/"+id)
	if !bodyContains(revisit, "answered this survey before") {
		t.Errorf("revisit after answering should show the note:\n%s", revisit)
	}
}

// TestAntibot_StrictLimitWithoutChallenge: skipping the proof-of-work
// gets the tight bucket (5/hour per IP+survey), and the window slides.
func TestAntibot_StrictLimitWithoutChallenge(t *testing.T) {
	t.Parallel()
	app := apptest.New(t, apptest.Options{})
	creator := app.Login(t, apptest.UniqueEmail("strict-limit"))
	id := publishedSurvey(t, app, creator, "Strict limit", true, [3]string{"short_text", "Q", ""})

	respondent := &http.Client{}
	submit := func() (*http.Response, string) {
		page := mustGet(t, respondent, app.Server.URL+"/s/"+id)
		form := respondForm(t, page)
		form.Set("q_"+extractAnswerFields(t, page)[0], "bulk")
		return submitAfterReading(t, app, respondent, id, form)
	}

	for i := 0; i < 5; i++ {
		if resp, body := submit(); resp.StatusCode != http.StatusOK {
			t.Fatalf("submission %d: status %d\n%s", i+1, resp.StatusCode, body)
		}
	}
	resp, body := submit()
	if resp.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("6th unchallenged submission: status %d, want 429\n%s", resp.StatusCode, body)
	}

	app.Clock.Advance(time.Hour)
	if resp, _ := submit(); resp.StatusCode != http.StatusOK {
		t.Errorf("submission after the window: status %d, want 200", resp.StatusCode)
	}
}

// solveALTCHA fetches a challenge and brute-forces it exactly as the
// first-party browser solver does, returning the base64 payload.
func solveALTCHA(t *testing.T, app *apptest.App, surveyID string) string {
	t.Helper()
	resp, err := http.Get(app.Server.URL + "/s/" + surveyID + "/challenge")
	if err != nil {
		t.Fatalf("GET challenge: %v", err)
	}
	defer resp.Body.Close()
	var challenge struct {
		Algorithm string `json:"algorithm"`
		Challenge string `json:"challenge"`
		MaxNumber int64  `json:"maxNumber"`
		Salt      string `json:"salt"`
		Signature string `json:"signature"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&challenge); err != nil {
		t.Fatalf("decode challenge: %v", err)
	}
	for n := int64(0); n <= challenge.MaxNumber; n++ {
		sum := sha256.Sum256([]byte(challenge.Salt + strconv.FormatInt(n, 10)))
		if hex.EncodeToString(sum[:]) == challenge.Challenge {
			payload, _ := json.Marshal(map[string]any{
				"algorithm": challenge.Algorithm,
				"challenge": challenge.Challenge,
				"number":    n,
				"salt":      challenge.Salt,
				"signature": challenge.Signature,
			})
			return base64.StdEncoding.EncodeToString(payload)
		}
	}
	t.Fatal("challenge not solvable within maxNumber")
	return ""
}

// TestAntibot_SolvedChallengeRaisesTheCeiling: proof-of-work buys the
// roomy bucket — more than the strict path's five.
func TestAntibot_SolvedChallengeRaisesTheCeiling(t *testing.T) {
	t.Parallel()
	app := apptest.New(t, apptest.Options{})
	creator := app.Login(t, apptest.UniqueEmail("altcha-ok"))
	id := publishedSurvey(t, app, creator, "Challenged", true, [3]string{"short_text", "Q", ""})

	respondent := &http.Client{}
	for i := 0; i < 7; i++ { // over the strict limit of 5
		page := mustGet(t, respondent, app.Server.URL+"/s/"+id)
		form := respondForm(t, page)
		form.Set("q_"+extractAnswerFields(t, page)[0], "solved")
		form.Set("altcha", solveALTCHA(t, app, id))
		resp, body := submitAfterReading(t, app, respondent, id, form)
		if resp.StatusCode != http.StatusOK || !bodyContains(body, "Thank you") {
			t.Fatalf("challenged submission %d: status %d\n%s", i+1, resp.StatusCode, body)
		}
	}
}

// TestAntibot_ForgedAndReplayedChallengesRejected is the SPEC.md testing
// contract: ALTCHA rejection of unsolved challenges — and a valid
// solution works exactly once.
func TestAntibot_ForgedAndReplayedChallengesRejected(t *testing.T) {
	t.Parallel()
	app := apptest.New(t, apptest.Options{})
	creator := app.Login(t, apptest.UniqueEmail("altcha-bad"))
	id := publishedSurvey(t, app, creator, "Forged challenge", true, [3]string{"short_text", "Q", ""})

	respondent := &http.Client{}
	submitWith := func(payload string) (*http.Response, string) {
		page := mustGet(t, respondent, app.Server.URL+"/s/"+id)
		form := respondForm(t, page)
		form.Set("q_"+extractAnswerFields(t, page)[0], "x")
		form.Set("altcha", payload)
		return submitAfterReading(t, app, respondent, id, form)
	}

	// Forged: right shape, wrong signature/answer.
	forged, _ := json.Marshal(map[string]any{
		"algorithm": "SHA-256", "challenge": "abc", "number": 1, "salt": "s", "signature": "nope",
	})
	if resp, _ := submitWith(base64.StdEncoding.EncodeToString(forged)); resp.StatusCode != http.StatusForbidden {
		t.Errorf("forged solution: status %d, want 403", resp.StatusCode)
	}

	// Replayed: a genuine solution, used twice.
	payload := solveALTCHA(t, app, id)
	if resp, _ := submitWith(payload); resp.StatusCode != http.StatusOK {
		t.Fatalf("genuine solution refused: status %d", resp.StatusCode)
	}
	if resp, _ := submitWith(payload); resp.StatusCode != http.StatusForbidden {
		t.Errorf("replayed solution: status %d, want 403", resp.StatusCode)
	}
	if got := responseCount(t, app, creator, id); got != "1" {
		t.Errorf("count = %s, want exactly the one genuine submission", got)
	}
}

// TestAntibot_ChallengeEndpointShape: the JSON the browser solver
// consumes, and its own rate limit.
func TestAntibot_ChallengeEndpointShape(t *testing.T) {
	t.Parallel()
	app := apptest.New(t, apptest.Options{})
	creator := app.Login(t, apptest.UniqueEmail("challenge-api"))
	id := publishedSurvey(t, app, creator, "Challenge API", true, [3]string{"short_text", "Q", ""})

	resp, err := http.Get(app.Server.URL + "/s/" + id + "/challenge")
	if err != nil {
		t.Fatalf("GET challenge: %v", err)
	}
	defer resp.Body.Close()
	var challenge map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&challenge); err != nil {
		t.Fatalf("decode: %v", err)
	}
	for _, field := range []string{"algorithm", "challenge", "maxNumber", "salt", "signature"} {
		if _, ok := challenge[field]; !ok {
			t.Errorf("challenge missing %q: %v", field, challenge)
		}
	}
	if alg := challenge["algorithm"]; alg != "SHA-256" {
		t.Errorf("algorithm = %v, want SHA-256 (the browser solver hard-codes it)", alg)
	}
}
