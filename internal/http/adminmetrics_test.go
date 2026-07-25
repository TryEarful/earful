package http_test

import (
	"net/http"
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
