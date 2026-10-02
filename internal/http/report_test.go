package http_test

import (
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"testing"

	"github.com/TryEarful/earful/internal/apptest"
)

// Reporting a survey, and the disclosure no style can remove (ADR-0018,
// its safeguards).

var reportLinkRe = regexp.MustCompile(`<a class="js-report-survey" href="([^"]+)">`)

// reportLink is the address of the report link on a page, unescaped as a
// browser reads the attribute, or empty.
func reportLink(t *testing.T, page string) string {
	t.Helper()
	m := reportLinkRe.FindStringSubmatch(page)
	if m == nil {
		return ""
	}
	return strings.ReplaceAll(m[1], "&amp;", "&")
}

// TestReport_TheFooterWritesToTheContactAboutTheSurvey: every page of a
// survey carries a link that writes to the instance's contact, with the
// survey's share link in the message. It is a mailto:, so the page
// still requests nothing from another origin.
func TestReport_TheFooterWritesToTheContactAboutTheSurvey(t *testing.T) {
	t.Parallel()
	app := apptest.New(t, apptest.Options{ContactEmail: "support@example.com"})
	creator := app.Login(t, apptest.UniqueEmail("reported"))
	id := styledSurvey(t, app, creator, "Reported")

	page := mustGet(t, jarClient(t), app.Server.URL+"/s/"+id)
	link := reportLink(t, page)
	if !bodyContains(page, "Report this survey") || link == "" {
		t.Fatalf("the survey's page has no report link:\n%s", page)
	}
	parsed, err := url.Parse(link)
	if err != nil {
		t.Fatalf("the report link is not an address: %v", err)
	}
	if parsed.Scheme != "mailto" || parsed.Opaque != "support@example.com" {
		t.Errorf("the report link writes to %q, want mailto:support@example.com", link)
	}
	query, err := url.ParseQuery(parsed.RawQuery)
	if err != nil {
		t.Fatalf("the report link's message is not encoded: %v", err)
	}
	if query.Get("subject") != "Report about a survey" {
		t.Errorf("the report's subject is %q", query.Get("subject"))
	}
	if body := query.Get("body"); !strings.Contains(body, app.Server.URL+"/s/"+id) && !strings.Contains(body, "/s/"+id) {
		t.Errorf("the report's message does not name the survey: %q", body)
	}
	if strings.Contains(parsed.RawQuery, "+") {
		t.Errorf("a space in the report link is a plus, which a mail client shows as one: %s", parsed.RawQuery)
	}
	for _, external := range findExternalURLs(page) {
		if external != reportLinkRe.FindStringSubmatch(page)[1] {
			t.Errorf("a survey's page references another origin: %s", external)
		}
	}

	// The thanks page carries it too.
	if thanks := answerOnce(t, app, "/s/"+id, "fine"); reportLink(t, thanks) != link {
		t.Errorf("the thanks page has no report link, or another one")
	}
	// A creator's own pages do not.
	if editor := mustGet(t, creator, app.Server.URL+"/surveys/"+id); strings.Contains(editor, "js-report-survey") {
		t.Errorf("a creator's page has the respondent's report link")
	}
}

// TestReport_APersonalLinkIsNeverPutInTheMessage: on a personal
// invitation's page the report names the survey's share link, since the
// invitation's address is its participant's credential.
func TestReport_APersonalLinkIsNeverPutInTheMessage(t *testing.T) {
	t.Parallel()
	app := apptest.New(t, apptest.Options{ContactEmail: "support@example.com"})
	creator := app.Login(t, apptest.UniqueEmail("reported-invited"))
	id := invitedSurvey(t, app, creator, "Invited and reported")
	invitee := apptest.UniqueEmail("reporting-invitee")
	app.PostForm(t, creator, "/surveys/"+id+"/participants", url.Values{"emails": {invitee}}).Body.Close()
	sendInvites(t, app, creator, id)
	invite := inviteLinkTo(t, app, invitee)
	token := invite[strings.LastIndex(invite, "/")+1:]

	page := mustGet(t, jarClient(t), invite)
	link := reportLink(t, page)
	if link == "" {
		t.Fatalf("an invitation's page has no report link:\n%s", page)
	}
	if strings.Contains(link, token) {
		t.Errorf("the report link carries the invitation's token: %s", link)
	}
	if !strings.Contains(link, url.QueryEscape("/s/"+id)) && !strings.Contains(link, "%2Fs%2F"+id) {
		t.Errorf("the report link does not name the survey's share link: %s", link)
	}
}

// TestReport_NoContactNoLink: an instance that publishes no contact
// shows no report link, rather than one that goes nowhere. The address
// mail is sent from is not a contact: on many instances nobody reads it,
// and a report sent there would be lost while the reporter believed it
// made.
func TestReport_NoContactNoLink(t *testing.T) {
	t.Parallel()
	app := apptest.New(t, apptest.Options{EmailFrom: "earful@mail.example.com"})
	creator := app.Login(t, apptest.UniqueEmail("unreportable"))
	id := styledSurvey(t, app, creator, "Nobody to tell")
	if page := mustGet(t, jarClient(t), app.Server.URL+"/s/"+id); strings.Contains(page, "js-report-survey") || bodyContains(page, "Report this survey") {
		t.Errorf("a report link with nobody to write to:\n%s", page)
	}
}

// TestDisclosure_NoStyleRemovesIt: with every part of a style set, the
// page still says, in Earful's words and before the questions, which
// workspace receives the answers, under the workspace's real name and
// not the name the header claims.
func TestDisclosure_NoStyleRemovesIt(t *testing.T) {
	t.Parallel()
	app := apptest.New(t, apptest.Options{})
	creator := app.Login(t, apptest.UniqueEmail("disclosed"))
	app.PostForm(t, creator, "/account/workspace", url.Values{"name": {"Real Owner Ltd"}}).Body.Close()
	id := styledSurvey(t, app, creator, "Fully styled")
	form := pictureForm("Corner Workshop")
	form.Set("thanks_picture", "confetti")
	postPictures(t, app, creator, id, form, ownLogo(t), bannerUpload(t))
	app.Publish(t, creator, id)

	resp, err := http.Get(app.Server.URL + "/s/" + id)
	if err != nil {
		t.Fatal(err)
	}
	page := apptest.ReadBody(t, resp)
	resp.Body.Close()
	if headerOf(page) == "" || pictureSrc(page, logoImgRe) == "" || footerOf(page) == "" {
		t.Fatalf("the survey is not fully styled, so the test would prove nothing:\n%s", page)
	}
	disclosure := strings.Index(page, "js-disclosure")
	questions := strings.Index(page, "js-respond-questions")
	if disclosure < 0 || questions < 0 || disclosure > questions {
		t.Fatalf("the disclosure is not on the page before the questions (at %d, questions at %d)", disclosure, questions)
	}
	end := strings.Index(page[disclosure:], "</div>")
	text := page[disclosure : disclosure+end]
	if !bodyContains(text, "Real Owner Ltd") {
		t.Errorf("the disclosure does not name the real workspace:\n%s", text)
	}
	if bodyContains(text, "Corner Workshop") {
		t.Errorf("the disclosure took the header's name:\n%s", text)
	}
}
