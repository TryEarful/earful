package http_test

import (
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/TryEarful/earful/internal/apptest"
	"github.com/TryEarful/earful/internal/auth"
	"github.com/TryEarful/earful/internal/email"
	"github.com/TryEarful/earful/internal/export"
)

var closureLinkRe = regexp.MustCompile(`https?://\S+/exports/closure/[A-Za-z0-9_-]+`)

// closeAccount presses "Delete my account", with or without asking for a
// copy, and returns the page it lands on.
func closeAccount(t *testing.T, app *apptest.App, client *http.Client, sendCopy bool) (*http.Response, string) {
	t.Helper()
	form := url.Values{}
	if sendCopy {
		form.Set("send_copy", "yes")
	}
	resp := app.PostForm(t, client, "/account/delete", form)
	body := apptest.ReadBody(t, resp)
	resp.Body.Close()
	if resp.Request.URL.Path != "/goodbye" {
		t.Fatalf("closing the account landed on %s, want /goodbye", resp.Request.URL.Path)
	}
	return resp, body
}

// waitForClosureEmail reads the inbox until the closure export's email
// arrives: the archive is built after the request has been answered.
func waitForClosureEmail(t *testing.T, app *apptest.App, addr string) email.Message {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		for _, msg := range app.Emails.To(addr) {
			if strings.Contains(msg.Subject, "copy of your Earful data") {
				return msg
			}
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("no closure export email reached %s", addr)
	return email.Message{}
}

func getClosure(t *testing.T, link string) (*http.Response, []byte) {
	t.Helper()
	// A plain client: no cookie jar, so no session.
	resp, err := (&http.Client{}).Get(link)
	if err != nil {
		t.Fatalf("GET %s: %v", link, err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	return resp, body
}

// TestClosureExport_EmailsALinkThatWorksWithoutASession is the closure
// copy end to end: asked for on the delete form, built after the
// account and its sessions are gone, emailed to the address the account
// had, downloadable by the link alone for 7 days and not after.
func TestClosureExport_EmailsALinkThatWorksWithoutASession(t *testing.T) {
	t.Parallel()
	app := apptest.New(t, apptest.Options{})
	addr := apptest.UniqueEmail("closure")
	client := app.Login(t, addr)

	page := getBody(t, client, app.Server.URL+"/account")
	if !bodyContains(page, `name="send_copy"`) || !bodyContains(page, "Send me a copy of my data") {
		t.Fatalf("the delete form offers no copy of the data:\n%s", page)
	}
	if box := regexp.MustCompile(`<input[^>]*name="send_copy"[^>]*>`).FindString(page); strings.Contains(box, "checked") {
		t.Error("the copy is asked for by default; it should be the person's choice")
	}

	id := app.CreateSurvey(t, client, "Kept in the copy", true)
	app.AddQuestion(t, client, id, "long_text", "What should travel with me?", nil)
	app.Publish(t, client, id)
	answerSurvey(t, app, id, map[int]string{0: "Every answer"})

	_, goodbye := closeAccount(t, app, client, true)
	if !bodyContains(goodbye, "A copy of your data is on its way") {
		t.Errorf("the goodbye page does not say a copy is coming:\n%s", goodbye)
	}
	// The account is closed whether or not a copy was asked for.
	if resp, err := client.Get(app.Server.URL + "/dashboard"); err != nil {
		t.Fatalf("dashboard after closing: %v", err)
	} else {
		resp.Body.Close()
		if resp.Request.URL.Path != "/login" {
			t.Errorf("the session survived the closure (landed on %s)", resp.Request.URL.Path)
		}
	}

	msg := waitForClosureEmail(t, app, addr)
	for _, want := range []string{"7 days", "forward"} {
		if !strings.Contains(msg.Text, want) {
			t.Errorf("the email does not mention %q:\n%s", want, msg.Text)
		}
	}
	link := closureLinkRe.FindString(msg.Text)
	if link == "" {
		t.Fatalf("no download link in the email:\n%s", msg.Text)
	}

	resp, body := getClosure(t, link)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("the emailed link answered %d without a session, want 200", resp.StatusCode)
	}
	if got := resp.Header.Get("Content-Type"); got != "application/zip" {
		t.Errorf("Content-Type = %q", got)
	}
	if got := resp.Header.Get("Cache-Control"); got != "no-store" {
		t.Errorf("Cache-Control = %q, want no-store", got)
	}
	if got := resp.Header.Get("Content-Disposition"); !strings.HasPrefix(got, "attachment") {
		t.Errorf("Content-Disposition = %q, want an attachment", got)
	}
	var archive export.Archive
	if err := json.Unmarshal(openArchive(t, body)["workspace.json"], &archive); err != nil {
		t.Fatalf("workspace.json does not match the documented format: %v", err)
	}
	var found *export.Survey
	for i := range archive.Surveys {
		if archive.Surveys[i].ID == id {
			found = &archive.Surveys[i]
		}
	}
	if found == nil {
		t.Fatalf("the closed workspace's survey is missing from its copy")
	}
	if len(found.Responses) != 1 {
		t.Errorf("the copy holds %d responses, want 1", len(found.Responses))
	}

	// A token nobody was sent is a plain 404, like any other miss.
	if resp, _ := getClosure(t, app.Server.URL+"/exports/closure/"+auth.NewToken()); resp.StatusCode != http.StatusNotFound {
		t.Errorf("an unknown token answered %d, want 404", resp.StatusCode)
	}

	// Six days on it still works; past the seventh it does not.
	app.Clock.Advance(6 * 24 * time.Hour)
	if resp, _ := getClosure(t, link); resp.StatusCode != http.StatusOK {
		t.Errorf("the link stopped working after 6 days (%d)", resp.StatusCode)
	}
	app.Clock.Advance(24*time.Hour + time.Minute)
	resp, body = getClosure(t, link)
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("the link still answers %d after 7 days, want 404", resp.StatusCode)
	}
	if strings.Contains(string(body), "What should travel with me?") {
		t.Error("an expired link served the archive")
	}
}

// TestClosureExport_SaysWhyWhenTheCopyCannotBeMade: the person can no
// longer sign in to see a failure, so the email says so instead of
// carrying a link.
func TestClosureExport_SaysWhyWhenTheCopyCannotBeMade(t *testing.T) {
	t.Parallel()
	app := apptest.New(t, apptest.Options{ExportMaxBytes: 1})
	addr := apptest.UniqueEmail("closure-large")
	client := app.Login(t, addr)

	closeAccount(t, app, client, true)
	msg := waitForClosureEmail(t, app, addr)
	if !strings.Contains(msg.Subject, "could not be made") {
		t.Errorf("subject = %q, want the failure", msg.Subject)
	}
	if !strings.Contains(msg.Text, "too large") {
		t.Errorf("the email does not say why:\n%s", msg.Text)
	}
	if strings.Contains(msg.Text, "/exports/closure/") {
		t.Errorf("a failed copy was sent with a link:\n%s", msg.Text)
	}
}

// TestClosureExport_NotAskedForSendsNothing: the box is unticked by
// default, and closing without it builds and sends nothing.
func TestClosureExport_NotAskedForSendsNothing(t *testing.T) {
	t.Parallel()
	app := apptest.New(t, apptest.Options{})
	addr := apptest.UniqueEmail("closure-none")
	client := app.Login(t, addr)
	before := len(app.Emails.To(addr))

	_, goodbye := closeAccount(t, app, client, false)
	if bodyContains(goodbye, "A copy of your data is on its way") {
		t.Errorf("the goodbye page promises a copy nobody asked for:\n%s", goodbye)
	}
	// Nothing is queued when the box is unticked, so there is no build
	// still running whose email could arrive later.
	if after := len(app.Emails.To(addr)); after != before {
		t.Errorf("closing without a copy sent %d emails", after-before)
	}
}
