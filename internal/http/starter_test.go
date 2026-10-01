package http_test

import (
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/TryEarful/earful/internal/apptest"
)

// Story 86: a workspace is created holding one published survey, the
// Starter Survey, which is its owner's to reword or delete.

const (
	starterTitle = "How is Earful working for you?"
	starterNote  = "A sample survey in English and Spanish."
)

var (
	starterQuestions = []string{
		"How did you hear about Earful?",
		"How satisfied are you with Earful so far, from 1 (not at all) to 5 (very)?",
		"How often do you expect to run a survey?",
		"What is missing from Earful, or should work differently?",
		"How likely are you to recommend Earful to a friend or colleague?",
	}
	starterQuestionsInSpanish = []string{
		"¿Cómo conoció Earful?",
		"¿Cuál es su grado de satisfacción con Earful hasta ahora, de 1 (ninguno) a 5 (muy alto)?",
		"¿Con qué frecuencia piensa hacer una encuesta?",
		"¿Qué le falta a Earful, o qué debería funcionar de otra manera?",
		"¿Qué probabilidad hay de que recomiende Earful a un amigo o colega?",
	}
)

// assertStarterSurvey checks what every new workspace is promised: one
// open, anonymous survey on the dashboard, and an address a stranger can
// open it at, in either language.
func assertStarterSurvey(t *testing.T, app *apptest.App, owner *http.Client) string {
	t.Helper()
	card := surveyCard(t, getBody(t, owner, app.Server.URL+"/dashboard"), starterTitle)
	for _, want := range []string{"Open", "Anonymous", "version 1 · 5 questions", starterNote} {
		if !bodyContains(card, want) {
			t.Errorf("the starter survey's card does not say %q:\n%s", want, card)
		}
	}

	id := app.StarterSurveyID(t, owner)
	stranger := &http.Client{}

	original := mustGet(t, stranger, app.Server.URL+"/s/"+id)
	for _, question := range starterQuestions {
		if !bodyContains(original, question) {
			t.Errorf("the survey does not ask %q:\n%s", question, original)
		}
	}
	if !strings.Contains(original, `<option value="es"`) {
		t.Errorf("the survey does not offer its Spanish:\n%s", original)
	}

	spanish := mustGet(t, stranger, app.Server.URL+"/s/"+id+"?lang=es")
	for _, question := range starterQuestionsInSpanish {
		if !bodyContains(spanish, question) {
			t.Errorf("the survey in Spanish does not ask %q:\n%s", question, spanish)
		}
	}
	for _, option := range []string{"Cada semana", "Cada mes", "Unas pocas veces al año", "Solo esta vez"} {
		if !bodyContains(spanish, option) {
			t.Errorf("the survey in Spanish does not offer %q:\n%s", option, spanish)
		}
	}
	if !strings.Contains(spanish, `<html lang="es">`) {
		t.Errorf("the survey in Spanish does not declare its language:\n%s", spanish[:200])
	}
	return id
}

// TestStarterSurvey_ComesWithAWorkspaceMadeBySigningIn is the path every
// instance outside the private beta takes: the account is made when a
// sign-in link is first used.
func TestStarterSurvey_ComesWithAWorkspaceMadeBySigningIn(t *testing.T) {
	t.Parallel()
	app := apptest.New(t, apptest.Options{})
	owner := app.Login(t, apptest.UniqueEmail("starter"))

	id := assertStarterSurvey(t, app, owner)

	// It is a survey like any other: an answer given to it is the
	// owner's to read.
	answerSurvey(t, app, id, map[int]string{0: "A friend who runs a café told me", 1: "4", 4: "9"})
	results := getBody(t, owner, app.Server.URL+"/surveys/"+id+"/results")
	if !bodyContains(results, "A friend who runs a café told me") {
		t.Errorf("an answer to the starter survey is not in its results:\n%s", results)
	}
}

// TestStarterSurvey_ComesWithAWorkspaceMadeByInviteCode is the path the
// private beta takes, which creates its workspace in a transaction of
// its own.
func TestStarterSurvey_ComesWithAWorkspaceMadeByInviteCode(t *testing.T) {
	t.Parallel()
	app := apptest.New(t, apptest.Options{BetaMode: true})
	code := app.MintBetaCode(t, "starter survey")
	owner := app.SignupWithCode(t, apptest.UniqueEmail("starter-beta"), "a long enough password", code)

	assertStarterSurvey(t, app, owner)
}

// inSpanish sends every request the client makes as a browser set to
// Spanish would.
type inSpanish struct{ next http.RoundTripper }

func (s inSpanish) RoundTrip(r *http.Request) (*http.Response, error) {
	r = r.Clone(r.Context())
	r.Header.Set("Accept-Language", "es-ES,es;q=0.9")
	return s.next.RoundTrip(r)
}

// TestStarterSurvey_IsTheSameWhoeverSignsUp: the workspace's name is in
// the language its owner signed up in, and the survey is not. It is
// English with a Spanish translation for everyone, so that the address
// handed out means the same survey whoever made the workspace. What is
// said about it on the dashboard is interface text, and follows the
// reader.
func TestStarterSurvey_IsTheSameWhoeverSignsUp(t *testing.T) {
	t.Parallel()
	app := apptest.New(t, apptest.Options{})
	owner := app.BrowserClient(t)
	owner.Transport = inSpanish{http.DefaultTransport}
	address := apptest.UniqueEmail("hispanohablante")
	app.LoginWithClient(t, owner, address)

	dashboard := getBody(t, owner, app.Server.URL+"/dashboard")
	local, _, _ := strings.Cut(address, "@")
	if !bodyContains(dashboard, "Espacio de trabajo de "+local) {
		t.Fatalf("the workspace was not named in Spanish, so this test is not reading as a Spanish browser:\n%s", dashboard)
	}
	card := surveyCard(t, dashboard, starterTitle)
	if !bodyContains(card, "Una encuesta de ejemplo en inglés y español.") {
		t.Errorf("the card does not describe the survey in the reader's language:\n%s", card)
	}

	id := app.StarterSurveyID(t, owner)
	original := mustGet(t, &http.Client{}, app.Server.URL+"/s/"+id)
	if !bodyContains(original, starterQuestions[0]) {
		t.Errorf("the survey's original is not English:\n%s", original)
	}
}

// TestStarterSurvey_IsGivenOnce: signing in again is not signing up, and
// two workspaces hold two surveys with nothing in common but their
// wording.
func TestStarterSurvey_IsGivenOnce(t *testing.T) {
	t.Parallel()
	app := apptest.New(t, apptest.Options{})
	address := apptest.UniqueEmail("starter-once")

	first := app.StarterSurveyID(t, app.Login(t, address))
	again := app.Login(t, address)
	// surveyCard fails the test on a title listed twice.
	surveyCard(t, getBody(t, again, app.Server.URL+"/dashboard"), starterTitle)
	if got := app.StarterSurveyID(t, again); got != first {
		t.Errorf("signing in again changed the starter survey from %s to %s", first, got)
	}

	other := app.StarterSurveyID(t, app.Login(t, apptest.UniqueEmail("starter-other")))
	if other == first {
		t.Fatal("two workspaces hold the same survey")
	}
	stranger := &http.Client{}
	mine := extractAnswerFields(t, mustGet(t, stranger, app.Server.URL+"/s/"+first))
	theirs := extractAnswerFields(t, mustGet(t, stranger, app.Server.URL+"/s/"+other))
	for _, identity := range mine {
		for _, otherIdentity := range theirs {
			if identity == otherIdentity {
				t.Errorf("two surveys share the question identity %s", identity)
			}
		}
	}
}

// TestStarterSurvey_IsItsOwnersToReword: the owner changes a question
// and publishes. The Spanish was made from the old wording, so it is
// read again first, as any translation would be (story 23).
func TestStarterSurvey_IsItsOwnersToReword(t *testing.T) {
	t.Parallel()
	app := apptest.New(t, apptest.Options{})
	owner := app.Login(t, apptest.UniqueEmail("starter-reword"))
	id := app.StarterSurveyID(t, owner)

	identity := app.QuestionIdentities(t, owner, id)[0]
	app.PostForm(t, owner, "/surveys/"+id+"/questions/"+identity, url.Values{
		"type": {"long_text"}, "text": {"How did you first hear about Kettle & Crow?"},
	}).Body.Close()

	if body := app.Publish(t, owner, id); !bodyContains(body, "Review every translation") {
		t.Errorf("a reworded question was published with the Spanish of the old one:\n%s", body)
	}
	app.PostForm(t, owner, "/surveys/"+id+"/localizations/es", url.Values{
		"t_" + identity: {"¿Cómo conoció Kettle & Crow?"},
	}).Body.Close()
	if body := app.Publish(t, owner, id); !bodyContains(body, "Published version 2") {
		t.Fatalf("the reworded survey was not published:\n%s", body)
	}

	stranger := &http.Client{}
	if page := mustGet(t, stranger, app.Server.URL+"/s/"+id); !bodyContains(page, "How did you first hear about Kettle & Crow?") {
		t.Errorf("respondents do not see the owner's wording:\n%s", page)
	}
	if page := mustGet(t, stranger, app.Server.URL+"/s/"+id+"?lang=es"); !bodyContains(page, "¿Cómo conoció Kettle & Crow?") {
		t.Errorf("respondents reading Spanish do not see the owner's wording:\n%s", page)
	}

	// The survey is now one its owner wrote, and is not introduced to
	// them as something they were given.
	card := surveyCard(t, getBody(t, owner, app.Server.URL+"/dashboard"), starterTitle)
	if bodyContains(card, starterNote) {
		t.Errorf("the card still introduces a survey its owner has rewritten:\n%s", card)
	}
}

// TestStarterSurvey_IsItsOwnersToDelete: deleted, it is gone like any
// other survey, and signing in again does not bring it back.
func TestStarterSurvey_IsItsOwnersToDelete(t *testing.T) {
	t.Parallel()
	app := apptest.New(t, apptest.Options{})
	address := apptest.UniqueEmail("starter-delete")
	owner := app.Login(t, address)
	id := app.StarterSurveyID(t, owner)

	app.PostForm(t, owner, "/surveys/"+id+"/delete", nil).Body.Close()

	if dashboard := getBody(t, owner, app.Server.URL+"/dashboard"); surveyCardRe.MatchString(dashboard) {
		t.Errorf("the dashboard still lists something after the only survey was deleted:\n%s", dashboard)
	}
	resp, err := http.Get(app.Server.URL + "/s/" + id)
	if err != nil {
		t.Fatalf("open the deleted survey: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("a deleted survey answered %d at its address, want 404", resp.StatusCode)
	}

	again := app.Login(t, address)
	if dashboard := getBody(t, again, app.Server.URL+"/dashboard"); surveyCardRe.MatchString(dashboard) {
		t.Errorf("signing in again brought back a survey its owner deleted:\n%s", dashboard)
	}
}
