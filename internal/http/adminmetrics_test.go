package http_test

import (
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"testing"

	"github.com/TryEarful/earful/internal/apptest"
)

// TestMetrics_AreSupportOnlyAndComeFromOurOwnData is M9-T7: the numbers
// exist, only a super admin can see them, and producing them costs a
// respondent nothing (ADR-0006).
func TestMetrics_AreSupportOnlyAndComeFromOurOwnData(t *testing.T) {
	t.Parallel()
	app := apptest.New(t, apptest.Options{})

	creator := app.Login(t, apptest.UniqueEmail("metrics-creator"))
	id := app.CreateSurvey(t, creator, "Measured survey", true)
	app.AddQuestion(t, creator, id, "short_text", "How was it?", nil)
	app.Publish(t, creator, id)
	answerSurvey(t, app, id, map[int]string{0: "fine"})

	// A non-admin cannot tell the page exists.
	resp, err := creator.Get(app.Server.URL + "/admin/metrics")
	if err != nil {
		t.Fatalf("GET as non-admin: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("non-admin status = %d, want 404", resp.StatusCode)
	}

	admin := app.LoginAsSuperAdmin(t, apptest.UniqueEmail("metrics-admin"))
	page := mustGet(t, admin, app.Server.URL+"/admin/metrics")

	for _, want := range []string{"Accounts", "Workspaces", "Surveys", "Responses", "AI spend"} {
		if !bodyContains(page, want) {
			t.Errorf("metrics page is missing %q:\n%s", want, page)
		}
	}
	// The page says where the numbers come from, which is the part that
	// keeps it honest about respondent privacy.
	if !bodyContains(page, "Nothing is added to respondent pages") {
		t.Errorf("the page does not state its own privacy position:\n%s", page)
	}

	// And a respondent page still loads nothing extra: the M4 check that
	// every URL is first-party covers this, so all that is asserted here
	// is that no analytics snippet appeared alongside the metrics work.
	respondent := &http.Client{}
	survey := mustGet(t, respondent, app.Server.URL+"/s/"+id)
	if external := findExternalURLs(survey); len(external) > 0 {
		t.Errorf("respondent page gained third-party URLs: %v", external)
	}
}

// metricTotal reads one of the totals off the metrics page.
func metricTotal(t *testing.T, page, label string) int {
	t.Helper()
	m := regexp.MustCompile(`<dt>` + regexp.QuoteMeta(label) + `</dt>\s*<dd>(\d+)</dd>`).FindStringSubmatch(page)
	if m == nil {
		t.Fatalf("no total labelled %q on the metrics page:\n%s", label, page)
	}
	n, err := strconv.Atoi(m[1])
	if err != nil {
		t.Fatalf("total %q: %v", label, err)
	}
	return n
}

// TestMetrics_CountAStarterSurveyOnceItsOwnerPublishesIt: every
// workspace is created holding a published survey (story 86). Were it
// counted, Surveys and Published surveys would rise with every signup
// and measure signups. It is counted from the version its owner
// publishes.
//
// The totals are of the whole instance, so this test has a database to
// itself (docs/testing.md) and is the only one using it.
func TestMetrics_CountAStarterSurveyOnceItsOwnerPublishesIt(t *testing.T) {
	t.Parallel()
	app := apptest.New(t, apptest.Options{DSN: apptest.NewIsolatedDB(t, "metrics")})
	admin := app.LoginAsSuperAdmin(t, apptest.UniqueEmail("metrics-counter"))

	read := func() (workspaces, surveys, published int) {
		page := mustGet(t, admin, app.Server.URL+"/admin/metrics")
		return metricTotal(t, page, "Workspaces"), metricTotal(t, page, "Surveys"), metricTotal(t, page, "Published surveys")
	}
	workspaces, surveys, published := read()

	expect := func(what string, wantWorkspaces, wantSurveys, wantPublished int) {
		t.Helper()
		gotWorkspaces, gotSurveys, gotPublished := read()
		if gotWorkspaces-workspaces != wantWorkspaces || gotSurveys-surveys != wantSurveys || gotPublished-published != wantPublished {
			t.Errorf("%s: workspaces %+d, surveys %+d, published %+d; want %+d, %+d, %+d", what,
				gotWorkspaces-workspaces, gotSurveys-surveys, gotPublished-published,
				wantWorkspaces, wantSurveys, wantPublished)
		}
	}

	creator := app.Login(t, apptest.UniqueEmail("metrics-signup"))
	expect("after a signup", 1, 0, 0)

	// A survey the creator makes is counted as it always was.
	own := app.CreateSurvey(t, creator, "Made by hand", true)
	app.AddQuestion(t, creator, own, "short_text", "How was it?", nil)
	expect("after a draft", 1, 1, 0)
	app.Publish(t, creator, own)
	expect("after publishing it", 1, 1, 1)

	// The Starter Survey, reworded and published, is the creator's doing.
	starter := app.StarterSurveyID(t, creator)
	identity := app.QuestionIdentities(t, creator, starter)[0]
	app.PostForm(t, creator, "/surveys/"+starter+"/questions/"+identity, url.Values{
		"type": {"long_text"}, "text": {"How did you find this shop?"},
	}).Body.Close()
	app.PostForm(t, creator, "/surveys/"+starter+"/localizations/es", url.Values{
		"t_" + identity: {"¿Cómo encontró esta tienda?"},
	}).Body.Close()
	if body := app.Publish(t, creator, starter); !bodyContains(body, "Published version 2") {
		t.Fatalf("the starter survey was not published again:\n%s", body)
	}
	expect("after publishing the starter survey again", 1, 2, 2)
}
