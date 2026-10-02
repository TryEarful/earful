package http_test

import (
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/TryEarful/earful/internal/apptest"
)

// Workspace suspension (ADR-0018, its safeguards): an operator stops a
// workspace that misuses the service, and lifting the suspension puts
// everything back as it was.

// suspendWorkspace has a super admin suspend the workspace of the
// account at address, and returns the admin's client and the
// workspace's id.
func suspendWorkspace(t *testing.T, app *apptest.App, address, reason string) (*http.Client, string) {
	t.Helper()
	admin := app.LoginAsSuperAdmin(t, apptest.UniqueEmail("suspender"))
	workspace := workspaceOf(t, app, admin, address)
	resp := app.PostForm(t, admin, "/admin/suspensions", url.Values{
		"email": {address}, "workspace": {workspace}, "reason": {reason},
	})
	body := apptest.ReadBody(t, resp)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK || !bodyContains(body, "Workspace suspended.") {
		t.Fatalf("suspending: status %d\n%s", resp.StatusCode, body)
	}
	return admin, workspace
}

// workspaceOf finds the workspace of the account at address on the
// operator's page.
func workspaceOf(t *testing.T, app *apptest.App, admin *http.Client, address string) string {
	t.Helper()
	page := mustGet(t, admin, app.Server.URL+"/admin/suspensions?email="+url.QueryEscape(address))
	// The workspaces found, after the list of every suspended one.
	found := page
	if i := strings.Index(page, "js-found-workspace"); i >= 0 {
		found = page[i:]
	}
	m := workspaceFieldRe.FindStringSubmatch(found)
	if m == nil {
		t.Fatalf("no workspace found for %s:\n%s", address, page)
	}
	return m[1]
}

func liftSuspension(t *testing.T, app *apptest.App, admin *http.Client, workspace string) {
	t.Helper()
	resp := app.PostForm(t, admin, "/admin/suspensions/lift", url.Values{"workspace": {workspace}})
	body := apptest.ReadBody(t, resp)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK || !bodyContains(body, "Suspension lifted.") {
		t.Fatalf("lifting: status %d\n%s", resp.StatusCode, body)
	}
}

// TestSuspension_RespondentsSeeEarfulsPlainPage: while a workspace is
// suspended, its survey takes no answers, a page opened before the
// suspension included, and is drawn as Earful's own page with none of
// its style: no theme, header, logo or footer, and its pictures are not
// served. Another workspace's survey goes on as before. Lifting the
// suspension puts the survey back as it was.
func TestSuspension_RespondentsSeeEarfulsPlainPage(t *testing.T) {
	t.Parallel()
	app := apptest.New(t, apptest.Options{})
	address := apptest.UniqueEmail("suspended-styled")
	creator := app.Login(t, address)
	id := styledSurvey(t, app, creator, "Impersonating")
	postPictures(t, app, creator, id, pictureForm("Corner Workshop"), ownLogo(t), bannerUpload(t))
	app.Publish(t, creator, id)

	bystander := app.Login(t, apptest.UniqueEmail("not-suspended"))
	other := styledSurvey(t, app, bystander, "Unaffected")

	respondent := jarClient(t)
	before := mustGet(t, respondent, app.Server.URL+"/s/"+id)
	logo := pictureSrc(before, logoImgRe)
	if headerOf(before) == "" || logo == "" || !strings.Contains(before, `class="theme-ocean"`) {
		t.Fatalf("the survey is not styled before the suspension, so the test would prove nothing:\n%s", before)
	}
	if got, _ := fetch(t, &http.Client{}, app.Server.URL+logo); got.StatusCode != http.StatusOK {
		t.Fatalf("the logo is not served before the suspension: %d", got.StatusCode)
	}
	open := respondForm(t, before)
	open.Set("q_"+extractAnswerFields(t, before)[0], "sent after the suspension")

	admin, workspace := suspendWorkspace(t, app, address, "Uses a bank's name and logo.")

	resp, err := respondent.Get(app.Server.URL + "/s/" + id)
	if err != nil {
		t.Fatal(err)
	}
	page := apptest.ReadBody(t, resp)
	resp.Body.Close()
	if resp.StatusCode != http.StatusGone || !bodyContains(page, "This survey isn't available") {
		t.Fatalf("a suspended workspace's survey: status %d\n%s", resp.StatusCode, page)
	}
	for _, leftover := range []string{"Corner Workshop", "Impersonating", "theme-ocean", "js-style-header", "js-style-footer", "js-style-logo"} {
		if strings.Contains(page, leftover) {
			t.Errorf("the suspended survey's page still shows %q:\n%s", leftover, page)
		}
	}
	if got, _ := fetch(t, &http.Client{}, app.Server.URL+logo); got.StatusCode != http.StatusNotFound {
		t.Errorf("a suspended workspace's logo is served: status %d, want 404", got.StatusCode)
	}

	resp, body := submitAfterReading(t, app, respondent, id, open)
	if resp.StatusCode != http.StatusGone || bodyContains(body, "Thank you") {
		t.Errorf("an answer sent from a page opened before the suspension: status %d\n%s", resp.StatusCode, body)
	}
	if results := mustGet(t, creator, app.Server.URL+"/surveys/"+id+"/results"); bodyContains(results, "sent after the suspension") {
		t.Errorf("an answer was recorded while the workspace was suspended")
	}

	if page := mustGet(t, jarClient(t), app.Server.URL+"/s/"+other); bodyContains(page, "isn't available") {
		t.Errorf("another workspace's survey was caught in the suspension:\n%s", page)
	}

	liftSuspension(t, app, admin, workspace)
	after := mustGet(t, jarClient(t), app.Server.URL+"/s/"+id)
	if headerOf(after) == "" || pictureSrc(after, logoImgRe) != logo || !strings.Contains(after, `class="theme-ocean"`) {
		t.Errorf("lifting the suspension did not put the survey back as it was:\n%s", after)
	}
	if got, _ := fetch(t, &http.Client{}, app.Server.URL+logo); got.StatusCode != http.StatusOK {
		t.Errorf("the logo is not served again after the lift: %d", got.StatusCode)
	}
	answerOnce(t, app, "/s/"+id, "after the lift")
}

// TestSuspension_InvitationLinksAndSendingAreHeld: a personal link to a
// suspended workspace's survey shows Earful's plain page, and its
// creator cannot send more invitations.
func TestSuspension_InvitationLinksAndSendingAreHeld(t *testing.T) {
	t.Parallel()
	app := apptest.New(t, apptest.Options{})
	address := apptest.UniqueEmail("suspended-invites")
	creator := app.Login(t, address)
	id := invitedSurvey(t, app, creator, "Invited")
	invitee := apptest.UniqueEmail("invitee")
	app.PostForm(t, creator, "/surveys/"+id+"/participants", url.Values{"emails": {invitee}}).Body.Close()
	sendInvites(t, app, creator, id)
	link := inviteLinkTo(t, app, invitee)

	suspendWorkspace(t, app, address, "Phishing for passwords.")

	resp, err := jarClient(t).Get(link)
	if err != nil {
		t.Fatal(err)
	}
	page := apptest.ReadBody(t, resp)
	resp.Body.Close()
	if resp.StatusCode != http.StatusGone || !bodyContains(page, "This survey isn't available") {
		t.Errorf("a personal link to a suspended workspace's survey: status %d\n%s", resp.StatusCode, page)
	}

	second := apptest.UniqueEmail("invitee-two")
	app.PostForm(t, creator, "/surveys/"+id+"/participants", url.Values{"emails": {second}}).Body.Close()
	resp = app.PostForm(t, creator, "/surveys/"+id+"/participants/send", nil)
	body := apptest.ReadBody(t, resp)
	resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden || !bodyContains(body, "This workspace is suspended") {
		t.Errorf("sending invitations while suspended: status %d\n%s", resp.StatusCode, body)
	}
	if len(app.Emails.To(second)) != 0 {
		t.Errorf("an invitation was sent while the workspace was suspended")
	}
}

// TestSuspension_CreatorsReadAndExportButCannotPublish: a suspended
// workspace's creator signs in and is told, on every page, what is held
// and whom to ask. They can still read, edit a draft and export; they
// cannot publish, reopen or use AI, and each is refused in words.
func TestSuspension_CreatorsReadAndExportButCannotPublish(t *testing.T) {
	t.Parallel()
	app := apptest.New(t, apptest.Options{AI: generatorFake(), ContactEmail: "support@example.com"})
	address := apptest.UniqueEmail("suspended-creator")
	creator := app.Login(t, address)
	id := styledSurvey(t, app, creator, "Held")
	closed := styledSurvey(t, app, creator, "Closed before")
	app.PostForm(t, creator, "/surveys/"+closed+"/close", nil).Body.Close()

	admin, workspace := suspendWorkspace(t, app, address, "Impersonation report confirmed.")

	dashboard := mustGet(t, creator, app.Server.URL+"/dashboard")
	if !bodyContains(dashboard, "This workspace is suspended.") ||
		!bodyContains(dashboard, "write to support@example.com") {
		t.Errorf("the dashboard does not say the workspace is suspended and whom to ask:\n%s", dashboard)
	}
	if editor := mustGet(t, creator, app.Server.URL+"/surveys/"+id); !strings.Contains(editor, "js-suspension-notice") {
		t.Errorf("the editor does not carry the suspension notice")
	}
	// An open survey takes no answers while suspended, and its card says
	// it is on hold rather than open.
	if !bodyContains(dashboard, "On hold") {
		t.Errorf("the dashboard shows an open survey of a suspended workspace as open:\n%s", dashboard)
	}

	// A draft can still be edited.
	app.AddQuestion(t, creator, id, "short_text", "A second question?", nil)
	if editor := mustGet(t, creator, app.Server.URL+"/surveys/"+id); !bodyContains(editor, "A second question?") {
		t.Errorf("a suspended creator could not edit their draft")
	}

	// Each refusal says which action was not done, and whom to ask.
	for action, refused := range map[string]string{
		"/surveys/" + id + "/publish":    "Your survey wasn't published.",
		"/surveys/" + closed + "/reopen": "Your survey wasn't reopened.",
	} {
		resp := app.PostForm(t, creator, action, nil)
		body := apptest.ReadBody(t, resp)
		resp.Body.Close()
		if resp.StatusCode != http.StatusForbidden || !bodyContains(body, "can't be published, reopened or sent") {
			t.Errorf("%s while suspended: status %d\n%s", action, resp.StatusCode, body)
		}
		if !bodyContains(body, refused+" Surveys can't") || !bodyContains(body, "write to support@example.com") {
			t.Errorf("%s while suspended does not say %q and whom to ask:\n%s", action, refused, body)
		}
	}

	resp := app.PostForm(t, creator, "/surveys/"+id+"/generate", url.Values{"prompt": {"more questions"}})
	body := apptest.ReadBody(t, resp)
	resp.Body.Close()
	if !bodyContains(body, "This workspace is suspended, so AI features are off") {
		t.Errorf("AI was not refused for a suspended workspace:\n%s", body)
	}

	// The workspace's data leaves with its owner, suspended or not.
	if link := waitForExport(t, app, creator); link == "" {
		t.Errorf("a suspended creator could not export their workspace")
	}

	// Nothing was published or reopened: after the lift, respondents
	// see the version from before, and the closed survey is still closed.
	liftSuspension(t, app, admin, workspace)
	if page := mustGet(t, jarClient(t), app.Server.URL+"/s/"+id); bodyContains(page, "A second question?") {
		t.Errorf("a version was published while the workspace was suspended")
	}
	if page := mustGet(t, jarClient(t), app.Server.URL+"/s/"+closed); !bodyContains(page, "This survey is closed") {
		t.Errorf("a survey was reopened while the workspace was suspended:\n%s", page)
	}
}

// TestSuspension_OnlyAnOperatorWithAReason: the page and its actions are
// a super admin's alone; a suspension needs a reason, is not taken twice,
// and a lift needs a suspension to lift.
func TestSuspension_OnlyAnOperatorWithAReason(t *testing.T) {
	t.Parallel()
	app := apptest.New(t, apptest.Options{})
	address := apptest.UniqueEmail("suspension-target")
	creator := app.Login(t, address)

	for _, req := range []func() *http.Response{
		func() *http.Response {
			r, err := creator.Get(app.Server.URL + "/admin/suspensions")
			if err != nil {
				t.Fatal(err)
			}
			return r
		},
		func() *http.Response {
			return app.PostForm(t, creator, "/admin/suspensions", url.Values{"workspace": {"00000000-0000-0000-0000-000000000000"}, "reason": {"x"}})
		},
		func() *http.Response {
			return app.PostForm(t, creator, "/admin/suspensions/lift", url.Values{"workspace": {"00000000-0000-0000-0000-000000000000"}})
		},
	} {
		r := req()
		r.Body.Close()
		if r.StatusCode != http.StatusNotFound {
			t.Errorf("a creator got status %d, want 404", r.StatusCode)
		}
	}

	admin := app.LoginAsSuperAdmin(t, apptest.UniqueEmail("suspension-admin"))
	if page := mustGet(t, admin, app.Server.URL+"/admin/suspensions"); !bodyContains(page, "No workspace is suspended") && !strings.Contains(page, "js-suspended") {
		t.Errorf("the operator's page does not list suspensions:\n%s", page)
	}
	workspace := workspaceOf(t, app, admin, address)

	resp := app.PostForm(t, admin, "/admin/suspensions", url.Values{"email": {address}, "workspace": {workspace}, "reason": {"   "}})
	body := apptest.ReadBody(t, resp)
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnprocessableEntity || !bodyContains(body, "Give a reason for the suspension.") {
		t.Errorf("a suspension without a reason: status %d\n%s", resp.StatusCode, body)
	}
	// The problem is said at the field, which points to it.
	if !strings.Contains(body, `id="reason-error-`+workspace+`"`) ||
		!strings.Contains(body, `aria-describedby="reason-error-`+workspace+` reason-hint-`+workspace+`"`) {
		t.Errorf("the missing reason is not said at its field:\n%s", body)
	}
	if dashboard := mustGet(t, creator, app.Server.URL+"/dashboard"); strings.Contains(dashboard, "js-suspension-notice") {
		t.Fatalf("a refused suspension suspended the workspace")
	}

	resp, err := admin.PostForm(app.Server.URL+"/admin/suspensions", url.Values{"workspace": {workspace}, "reason": {"No token."}})
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("a suspension without a CSRF token got %d, want 403", resp.StatusCode)
	}

	resp = app.PostForm(t, admin, "/admin/suspensions/lift", url.Values{"workspace": {workspace}})
	body = apptest.ReadBody(t, resp)
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnprocessableEntity || !bodyContains(body, "That workspace isn't suspended.") {
		t.Errorf("lifting a suspension that is not there: status %d\n%s", resp.StatusCode, body)
	}

	resp = app.PostForm(t, admin, "/admin/suspensions", url.Values{"email": {address}, "workspace": {workspace}, "reason": {"Spam surveys."}})
	resp.Body.Close()
	page := mustGet(t, admin, app.Server.URL+"/admin/suspensions")
	if !bodyContains(page, "Spam surveys.") || !bodyContains(page, address) {
		t.Errorf("the suspended workspace is not listed with its member and reason:\n%s", page)
	}
	resp = app.PostForm(t, admin, "/admin/suspensions", url.Values{"email": {address}, "workspace": {workspace}, "reason": {"Again."}})
	body = apptest.ReadBody(t, resp)
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnprocessableEntity || !bodyContains(body, "already suspended") {
		t.Errorf("suspending twice: status %d\n%s", resp.StatusCode, body)
	}
	if page := mustGet(t, admin, app.Server.URL+"/admin/suspensions"); !bodyContains(page, "Spam surveys.") || bodyContains(page, "Again.") {
		t.Errorf("a second suspension overwrote the first one's reason:\n%s", page)
	}
}
