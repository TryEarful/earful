package http_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/TryEarful/earful/internal/apptest"
)

// invitedSurvey creates a published invited survey with one long-text
// question and returns its id.
func invitedSurvey(t *testing.T, app *apptest.App, creator *http.Client, title string) string {
	t.Helper()
	id := app.CreateSurvey(t, creator, title, false)
	app.AddQuestion(t, creator, id, "long_text", "What do you think?", nil)
	app.Publish(t, creator, id)
	return id
}

var inviteLinkRe = regexp.MustCompile(`https?://\S+/p/[A-Za-z0-9_-]+`)

// inviteLinkTo reads addr's invite link out of the captured outbox.
func inviteLinkTo(t *testing.T, app *apptest.App, addr string) string {
	t.Helper()
	msgs := app.Emails.To(addr)
	if len(msgs) == 0 {
		t.Fatalf("no invite email to %s", addr)
	}
	link := inviteLinkRe.FindString(msgs[len(msgs)-1].Text)
	if link == "" {
		t.Fatalf("no invite link in email to %s:\n%s", addr, msgs[len(msgs)-1].Text)
	}
	return link
}

// sendInvites presses the send button and returns the editor page body.
func sendInvites(t *testing.T, app *apptest.App, creator *http.Client, surveyID string) string {
	t.Helper()
	resp := app.PostForm(t, creator, "/surveys/"+surveyID+"/participants/send", nil)
	defer resp.Body.Close()
	return apptest.ReadBody(t, resp)
}

// TestParticipants_ImportDedupes covers SPEC.md story 44: paste/CSV
// import with duplicates removed and junk skipped.
func TestParticipants_ImportDedupes(t *testing.T) {
	t.Parallel()
	app := apptest.New(t, apptest.Options{})
	creator := app.Login(t, apptest.UniqueEmail("import"))
	id := invitedSurvey(t, app, creator, "Import test")

	a, b := apptest.UniqueEmail("dupe-a"), apptest.UniqueEmail("dupe-b")
	resp := app.PostForm(t, creator, "/surveys/"+id+"/participants", url.Values{
		"emails": {a + "\n" + b + ", " + a + "; not-an-email\n" + strings.ToUpper(a)},
	})
	body := apptest.ReadBody(t, resp)
	resp.Body.Close()
	if !bodyContains(body, "Added 2 participants") {
		t.Errorf("expected 2 added (duplicates and junk skipped):\n%s", body)
	}
	if !bodyContains(body, "Skipped 1") {
		t.Errorf("expected 1 skipped entry:\n%s", body)
	}

	// Re-importing the same addresses adds nothing.
	resp = app.PostForm(t, creator, "/surveys/"+id+"/participants", url.Values{"emails": {a + "\n" + b}})
	body = apptest.ReadBody(t, resp)
	resp.Body.Close()
	if !bodyContains(body, "Added 0 participants") {
		t.Errorf("re-import should be a no-op:\n%s", body)
	}
}

// TestParticipants_InviteAndAnswerFlow covers stories 45, 47 and 49: a
// unique personal link arrives by email, answering works without an
// account, and the second use of the link hits the already-answered page.
func TestParticipants_InviteAndAnswerFlow(t *testing.T) {
	t.Parallel()
	app := apptest.New(t, apptest.Options{})
	creator := app.Login(t, apptest.UniqueEmail("invite-flow"))
	id := invitedSurvey(t, app, creator, "Invite flow")

	addr := apptest.UniqueEmail("participant")
	resp := app.PostForm(t, creator, "/surveys/"+id+"/participants", url.Values{"emails": {addr}})
	resp.Body.Close()

	body := sendInvites(t, app, creator, id)
	if !bodyContains(body, "Sent 1 invite") {
		t.Fatalf("send did not report success:\n%s", body)
	}
	if !bodyContains(body, "Invited") {
		t.Errorf("participant list should show Invited:\n%s", body)
	}

	// The participant opens their personal link — no account, no session.
	link := inviteLinkTo(t, app, addr)
	participant := &http.Client{}
	page := mustGet(t, participant, link)
	if !bodyContains(page, "What do you think?") {
		t.Fatalf("invite link did not render the survey:\n%s", page)
	}
	if !bodyContains(page, "answering as") || !bodyContains(page, addr) {
		t.Errorf("participant page should disclose who is answering:\n%s", page)
	}

	token := link[strings.LastIndex(link, "/")+1:]
	form := respondForm(t, page)
	form.Set("q_"+extractAnswerFields(t, page)[0], "I think it works")
	_, thanks := submitAfterReadingTo(t, app, participant, "/p/"+token, form)
	if !bodyContains(thanks, "Thank you") {
		t.Fatalf("participant submission failed:\n%s", thanks)
	}

	// One per person (story 47): the link is now spent.
	revisit := mustGet(t, participant, link)
	if !bodyContains(revisit, "already answered") {
		t.Errorf("second visit should hit the already-answered page:\n%s", revisit)
	}

	// The creator sees who answered (story 49) and the response count.
	editor := app.SurveyPage(t, creator, id)
	if !bodyContains(editor, "Submitted") {
		t.Errorf("participant list should show Submitted:\n%s", editor)
	}
	if !bodyContains(editor, "1 response") {
		t.Errorf("editor should count the response:\n%s", editor)
	}
}

// TestParticipants_ResubmitAfterAnswerRefused: even a crafted POST on a
// spent link stores nothing — the unique index owns the guarantee.
func TestParticipants_ResubmitAfterAnswerRefused(t *testing.T) {
	t.Parallel()
	app := apptest.New(t, apptest.Options{})
	creator := app.Login(t, apptest.UniqueEmail("respent"))
	id := invitedSurvey(t, app, creator, "Spent link")

	addr := apptest.UniqueEmail("one-shot")
	resp := app.PostForm(t, creator, "/surveys/"+id+"/participants", url.Values{"emails": {addr}})
	resp.Body.Close()
	sendInvites(t, app, creator, id)

	link := inviteLinkTo(t, app, addr)
	token := link[strings.LastIndex(link, "/")+1:]
	participant := &http.Client{}
	page := mustGet(t, participant, link)
	form := respondForm(t, page)
	form.Set("q_"+extractAnswerFields(t, page)[0], "first")
	submitAfterReadingTo(t, app, participant, "/p/"+token, form)

	// Replay the same form directly, skipping the GET.
	_, second := submitAfterReadingTo(t, app, participant, "/p/"+token, form)
	if !bodyContains(second, "already answered") {
		t.Errorf("crafted resubmission should land on already-answered:\n%s", second)
	}
	editor := app.SurveyPage(t, creator, id)
	if !bodyContains(editor, "1 response") {
		t.Errorf("resubmission changed the count:\n%s", editor)
	}
}

// TestParticipants_InvalidTokenIs404: guessing tokens gets nothing
// (story 45's ≥128-bit guarantee is in auth.NewToken; here we check the
// response gives no hints).
func TestParticipants_InvalidTokenIs404(t *testing.T) {
	t.Parallel()
	app := apptest.New(t, apptest.Options{})

	resp, err := http.Get(app.Server.URL + "/p/not-a-real-token")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("status = %d, want 404", resp.StatusCode)
	}
}

// TestParticipants_PublicLinkRefusesInvitedSurvey: invited surveys answer
// only through personal links.
func TestParticipants_PublicLinkRefusesInvitedSurvey(t *testing.T) {
	t.Parallel()
	app := apptest.New(t, apptest.Options{})
	creator := app.Login(t, apptest.UniqueEmail("invite-only"))
	id := invitedSurvey(t, app, creator, "No public entry")

	resp, err := http.Get(app.Server.URL + "/s/" + id)
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	body := apptest.ReadBody(t, resp)
	resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("GET status = %d, want 403", resp.StatusCode)
	}
	if !bodyContains(body, "invite-only") {
		t.Errorf("expected the invite-only explanation:\n%s", body)
	}
	if bodyContains(body, "What do you think?") {
		t.Error("the public link leaked the survey's questions")
	}
}

// TestParticipants_SuppressionHonored covers story 48 and the M4-T4 AC: a
// bounced address is never mailed again, across surveys.
func TestParticipants_SuppressionHonored(t *testing.T) {
	t.Parallel()
	app := apptest.New(t, apptest.Options{})
	creator := app.Login(t, apptest.UniqueEmail("suppression"))
	id := invitedSurvey(t, app, creator, "Suppression")

	bouncer := apptest.UniqueEmail("bouncer")
	healthy := apptest.UniqueEmail("healthy")
	resp := app.PostForm(t, creator, "/surveys/"+id+"/participants", url.Values{"emails": {bouncer + "\n" + healthy}})
	resp.Body.Close()

	// The ESP reports a hard bounce for one address (webhook, M4-T6).
	event, _ := json.Marshal(map[string]string{"event": "hard_bounce", "email": bouncer})
	whResp, err := http.Post(app.Server.URL+"/webhooks/email/"+apptest.WebhookSecret, "application/json", bytes.NewReader(event))
	if err != nil {
		t.Fatalf("webhook: %v", err)
	}
	whResp.Body.Close()
	if whResp.StatusCode != http.StatusOK {
		t.Fatalf("webhook status = %d, want 200", whResp.StatusCode)
	}

	body := sendInvites(t, app, creator, id)
	if !bodyContains(body, "Sent 1 invite") {
		t.Fatalf("expected exactly the healthy address to be mailed:\n%s", body)
	}
	if got := app.Emails.To(bouncer); len(got) != 0 {
		t.Errorf("suppressed address received %d emails, want 0", len(got))
	}
	if got := app.Emails.To(healthy); len(got) != 1 {
		t.Errorf("healthy address received %d emails, want 1", len(got))
	}

	// The suppression is global: the same address on a NEW survey is
	// skipped too.
	id2 := invitedSurvey(t, app, creator, "Second survey")
	resp = app.PostForm(t, creator, "/surveys/"+id2+"/participants", url.Values{"emails": {bouncer}})
	resp.Body.Close()
	body = sendInvites(t, app, creator, id2)
	if !bodyContains(body, "Sent 0 invites") {
		t.Errorf("suppressed address was mailed on a second survey:\n%s", body)
	}
}

// TestParticipants_WebhookSecretRequired: without the right path secret
// the endpoint plays dead and nothing is suppressed.
func TestParticipants_WebhookSecretRequired(t *testing.T) {
	t.Parallel()
	app := apptest.New(t, apptest.Options{})
	creator := app.Login(t, apptest.UniqueEmail("webhook-secret"))
	id := invitedSurvey(t, app, creator, "Webhook auth")

	victim := apptest.UniqueEmail("victim")
	resp := app.PostForm(t, creator, "/surveys/"+id+"/participants", url.Values{"emails": {victim}})
	resp.Body.Close()

	event, _ := json.Marshal(map[string]string{"event": "hard_bounce", "email": victim})
	whResp, err := http.Post(app.Server.URL+"/webhooks/email/wrong-secret", "application/json", bytes.NewReader(event))
	if err != nil {
		t.Fatalf("webhook: %v", err)
	}
	whResp.Body.Close()
	if whResp.StatusCode != http.StatusNotFound {
		t.Errorf("wrong secret: status = %d, want 404", whResp.StatusCode)
	}

	// The forged event suppressed nothing: the invite still goes out.
	if body := sendInvites(t, app, creator, id); !bodyContains(body, "Sent 1 invite") {
		t.Errorf("a forged webhook suppressed a healthy address:\n%s", body)
	}
}

// TestParticipants_FailedSendRetries: a sender error rolls the
// participant back to pending; the next run retries them.
func TestParticipants_FailedSendRetries(t *testing.T) {
	t.Parallel()
	app := apptest.New(t, apptest.Options{})
	creator := app.Login(t, apptest.UniqueEmail("retry"))
	id := invitedSurvey(t, app, creator, "Retry")

	flaky := apptest.UniqueEmail("flaky")
	resp := app.PostForm(t, creator, "/surveys/"+id+"/participants", url.Values{"emails": {flaky}})
	resp.Body.Close()

	app.Emails.FailFor(flaky)
	body := sendInvites(t, app, creator, id)
	if !bodyContains(body, "1 failed and will be retried") {
		t.Fatalf("expected the failure to be reported:\n%s", body)
	}

	// Provider recovers; the pending participant is retried.
	app.Emails.Recover(flaky)
	body = sendInvites(t, app, creator, id)
	if !bodyContains(body, "Sent 1 invite") {
		t.Errorf("retry did not send:\n%s", body)
	}
}

// TestParticipants_DripCap covers story 46: the hourly workspace cap
// holds, and the window slides.
func TestParticipants_DripCap(t *testing.T) {
	t.Parallel()
	app := apptest.New(t, apptest.Options{})
	creator := app.Login(t, apptest.UniqueEmail("drip"))
	id := invitedSurvey(t, app, creator, "Drip cap")

	// 200 fit the hourly cap; 3 spill over.
	var addresses []string
	for i := 0; i < 203; i++ {
		addresses = append(addresses, apptest.UniqueEmail("bulk"))
	}
	resp := app.PostForm(t, creator, "/surveys/"+id+"/participants", url.Values{"emails": {strings.Join(addresses, "\n")}})
	body := apptest.ReadBody(t, resp)
	resp.Body.Close()
	if !bodyContains(body, "Added 203 participants") {
		t.Fatalf("bulk import failed:\n%s", body)
	}

	body = sendInvites(t, app, creator, id)
	if !bodyContains(body, "Sent 200 invites") {
		t.Fatalf("expected the cap to allow exactly 200:\n%s", body)
	}
	if !bodyContains(body, "3 are waiting on the hourly sending cap") {
		t.Errorf("expected the overflow to be reported:\n%s", body)
	}
	// Count invites only — the outbox also holds the creator's own
	// magic-link email.
	invitesSent := 0
	for _, m := range app.Emails.All() {
		if strings.HasPrefix(m.Subject, "You're invited") {
			invitesSent++
		}
	}
	if invitesSent != 200 {
		t.Fatalf("outbox holds %d invites, want 200", invitesSent)
	}

	// Next hour, the remainder drains.
	app.Clock.Advance(61 * time.Minute)
	body = sendInvites(t, app, creator, id)
	if !bodyContains(body, "Sent 3 invites") {
		t.Errorf("overflow did not drain after the window:\n%s", body)
	}
}

// TestParticipants_CrossWorkspaceDenied: participant management is as
// workspace-scoped as everything else.
func TestParticipants_CrossWorkspaceDenied(t *testing.T) {
	t.Parallel()
	app := apptest.New(t, apptest.Options{})
	owner := app.Login(t, apptest.UniqueEmail("p-owner"))
	id := invitedSurvey(t, app, owner, "Private audience")

	intruder := app.Login(t, apptest.UniqueEmail("p-intruder"))
	for _, path := range []string{
		"/surveys/" + id + "/participants",
		"/surveys/" + id + "/participants/send",
	} {
		resp := app.PostForm(t, intruder, path, url.Values{"emails": {"stolen@example.test"}})
		resp.Body.Close()
		if resp.StatusCode != http.StatusNotFound {
			t.Errorf("POST %s: status = %d, want 404", path, resp.StatusCode)
		}
	}
}
