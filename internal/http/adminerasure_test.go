package http_test

import (
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/TryEarful/earful/internal/apptest"
)

// TestErasure_IsSupportOnlyAndTwoSteps is M8-T3's shape: only support can
// see it at all, and erasing is a confirmed second step after seeing
// exactly what would go.
func TestErasure_IsSupportOnlyAndTwoSteps(t *testing.T) {
	t.Parallel()
	app := apptest.New(t, apptest.Options{})

	// An ordinary creator with data to be erased.
	subject := apptest.UniqueEmail("subject")
	creator := app.Login(t, subject)
	id := app.CreateSurvey(t, creator, "Their survey", true)
	app.AddQuestion(t, creator, id, "short_text", "A question", nil)
	app.Publish(t, creator, id)
	answerSurvey(t, app, id, map[int]string{0: "an answer"})

	// A non-admin cannot see the page exists.
	resp, err := creator.Get(app.Server.URL + "/admin/erasure")
	if err != nil {
		t.Fatalf("GET as non-admin: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("non-admin got status %d, want 404", resp.StatusCode)
	}

	admin := app.LoginAsSuperAdmin(t, apptest.UniqueEmail("support"))

	// Step one: look up, and see what would go — without anything going.
	page := mustGet(t, admin, app.Server.URL+"/admin/erasure?email="+url.QueryEscape(subject))
	if !bodyContains(page, "What would be erased") {
		t.Fatalf("lookup showed nothing:\n%s", page)
	}
	if !bodyContains(page, "Their account") || !bodyContains(page, "workspace(s)") {
		t.Errorf("lookup did not describe what would go:\n%s", page)
	}
	// The honest note about anonymous responses is part of the page, not
	// something support has to know.
	if !bodyContains(page, "Anonymous responses are not listed") {
		t.Errorf("the page does not explain anonymous responses:\n%s", page)
	}
	// Nothing has been erased by looking.
	if body := mustGet(t, creator, app.Server.URL+"/surveys/"+id); !bodyContains(body, "A question") {
		t.Error("looking up a subject erased their data")
	}

	// Step two: confirm.
	resp = app.PostForm(t, admin, "/admin/erasure", url.Values{"email": {subject}})
	body := apptest.ReadBody(t, resp)
	resp.Body.Close()
	if !bodyContains(body, "Erasure complete") {
		t.Fatalf("erasure did not confirm:\n%s", body)
	}

	// A second lookup finds nothing left.
	page = mustGet(t, admin, app.Server.URL+"/admin/erasure?email="+url.QueryEscape(subject))
	if !bodyContains(page, "Nothing found") {
		t.Errorf("after erasure the subject still has data:\n%s", page)
	}

	// The address is free again: signing in creates a new, empty account
	// rather than resurrecting the old one.
	fresh := app.Login(t, subject)
	dashboard := mustGet(t, fresh, app.Server.URL+"/dashboard")
	if bodyContains(dashboard, "Their survey") {
		t.Error("the erased account's survey came back with a new sign-in")
	}
}

// TestErasure_RemovesAParticipantWithoutTouchingTheSurvey: an invited
// respondent can ask too, and erasing them must not erase the creator's
// survey.
func TestErasure_RemovesAParticipantWithoutTouchingTheSurvey(t *testing.T) {
	t.Parallel()
	app := apptest.New(t, apptest.Options{})
	creator := app.Login(t, apptest.UniqueEmail("erasure-creator"))
	id := app.CreateSurvey(t, creator, "Invited survey", false)
	app.AddQuestion(t, creator, id, "short_text", "Your take?", nil)
	app.Publish(t, creator, id)

	guest := apptest.UniqueEmail("erasure-guest")
	app.PostForm(t, creator, "/surveys/"+id+"/participants", url.Values{"emails": {guest}}).Body.Close()
	app.PostForm(t, creator, "/surveys/"+id+"/participants/send", nil).Body.Close()
	link := inviteLinkTo(t, app, guest)

	participant := &http.Client{}
	page := mustGet(t, participant, link)
	form := respondForm(t, page)
	form.Set("q_"+extractAnswerFields(t, page)[0], "An answer to be erased")
	submitAfterReadingTo(t, app, participant, strings.TrimPrefix(link, app.Server.URL), form)

	admin := app.LoginAsSuperAdmin(t, apptest.UniqueEmail("support2"))
	resp := app.PostForm(t, admin, "/admin/erasure", url.Values{"email": {guest}})
	resp.Body.Close()

	// The creator keeps their survey; the participant and their answer
	// are gone from it.
	results := mustGet(t, creator, app.Server.URL+"/surveys/"+id+"/results")
	if bodyContains(results, guest) || bodyContains(results, "An answer to be erased") {
		t.Errorf("the erased participant's data is still in the results:\n%s", results)
	}
	if body := mustGet(t, creator, app.Server.URL+"/surveys/"+id); !bodyContains(body, "Your take?") {
		t.Error("erasing a participant removed the creator's survey")
	}
}
