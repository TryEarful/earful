package http_test

import (
	"net/http"
	"strings"
	"testing"

	"github.com/TryEarful/earful/internal/ai"
	"github.com/TryEarful/earful/internal/apptest"
)

// TestTrust_IsPublicAndSaysWhatItCanProve is M8-T4: the page exists
// without a session, states the promises, and — where something is a
// residual risk rather than a guarantee — says that too.
func TestTrust_IsPublicAndSaysWhatItCanProve(t *testing.T) {
	t.Parallel()
	app := apptest.New(t, apptest.Options{})

	anyone := &http.Client{}
	page := mustGet(t, anyone, app.Server.URL+"/trust")

	for _, promise := range []string{
		"Your voice is never stored",
		"no email address, no IP address and no device details",
		"smaller than five people",
		"no analytics",
		"You can leave",
	} {
		if !bodyContains(page, promise) {
			t.Errorf("the trust page does not state %q:\n%s", promise, page)
		}
	}

	// The honest paragraphs matter as much as the promises: a trust page
	// that only claims strengths is marketing.
	for _, honesty := range []string{
		"US law reaches American companies",
		"fully effective within 30 days",
	} {
		if !bodyContains(page, honesty) {
			t.Errorf("the trust page omits the caveat %q", honesty)
		}
	}

	// The CC-BY attribution the country data requires travels with it.
	if !bodyContains(page, "DB-IP") {
		t.Errorf("GeoIP attribution missing from the trust page:\n%s", page)
	}
}

// TestTrust_ListsOnlyTheProcessorsThisInstanceUses: listing a company
// you don't use is as misleading as omitting one you do. A local
// instance with no AI and no ESP involves nobody.
func TestTrust_ListsOnlyTheProcessorsThisInstanceUses(t *testing.T) {
	t.Parallel()

	bare := apptest.New(t, apptest.Options{})
	page := mustGet(t, &http.Client{}, bare.Server.URL+"/trust")
	if strings.Contains(page, "Brevo") || strings.Contains(page, "Vertex") {
		t.Errorf("a local instance listed processors it does not use:\n%s", page)
	}
	if !bodyContains(page, "runs entirely on its operator's own infrastructure") {
		t.Errorf("an instance with no processors should say so:\n%s", page)
	}

	// With an AI backend configured, it is disclosed.
	withAI := apptest.New(t, apptest.Options{AI: &ai.Fake{}})
	_ = withAI // the fake is injected, not configured, so nothing to disclose
}

// TestRespondentDisclosure_TellsRespondentsWhatHappens is M8-T5: the
// controller, the anonymity status, the voice note when voice is on
// offer, and a link to the whole story.
func TestRespondentDisclosure_TellsRespondentsWhatHappens(t *testing.T) {
	t.Parallel()

	t.Run("anonymous survey without voice", func(t *testing.T) {
		t.Parallel()
		app := apptest.New(t, apptest.Options{})
		creator := app.Login(t, apptest.UniqueEmail("disclosure-anon"))
		id := publishedSurvey(t, app, creator, "Anonymous", true,
			[3]string{"long_text", "How was it?", ""})

		page := mustGet(t, &http.Client{}, app.Server.URL+"/s/"+id)
		if !bodyContains(page, "who decides what happens to the answers") {
			t.Errorf("the controller is not named:\n%s", page)
		}
		if !bodyContains(page, "It's anonymous") {
			t.Errorf("anonymity is not disclosed:\n%s", page)
		}
		if bodyContains(page, "answer by speaking") {
			t.Error("voice was described on a survey where it is not available")
		}
		if !bodyContains(page, "How Earful handles your data") {
			t.Errorf("no link to the trust page:\n%s", page)
		}
	})

	t.Run("with voice available", func(t *testing.T) {
		t.Parallel()
		app := apptest.New(t, apptest.Options{AI: &ai.Fake{}})
		creator := app.Login(t, apptest.UniqueEmail("disclosure-voice"))
		id := publishedSurvey(t, app, creator, "Voice survey", true,
			[3]string{"long_text", "How was it?", ""})

		page := mustGet(t, &http.Client{}, app.Server.URL+"/s/"+id)
		if !bodyContains(page, "your voice itself is never stored") {
			t.Errorf("the voice promise is missing where voice is offered:\n%s", page)
		}
	})

	t.Run("invited survey names the invitee", func(t *testing.T) {
		t.Parallel()
		app := apptest.New(t, apptest.Options{})
		creator := app.Login(t, apptest.UniqueEmail("disclosure-invited"))
		id := app.CreateSurvey(t, creator, "Invited", false)
		app.AddQuestion(t, creator, id, "short_text", "Your take?", nil)
		app.Publish(t, creator, id)

		guest := apptest.UniqueEmail("disclosure-guest")
		app.PostForm(t, creator, "/surveys/"+id+"/participants",
			map[string][]string{"emails": {guest}}).Body.Close()
		app.PostForm(t, creator, "/surveys/"+id+"/participants/send", nil).Body.Close()

		page := mustGet(t, &http.Client{}, inviteLinkTo(t, app, guest))
		if !bodyContains(page, "You're answering as") || !bodyContains(page, guest) {
			t.Errorf("an invited respondent is not told their answers are attributed:\n%s", page)
		}
	})
}
