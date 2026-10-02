package http

import (
	"net/http"
	"net/url"
	"strings"

	"github.com/google/uuid"

	"github.com/TryEarful/earful/internal/config"
	"github.com/TryEarful/earful/internal/store"
	"github.com/TryEarful/earful/internal/uitext"
	"github.com/TryEarful/earful/web/templates"
)

// Reporting a survey (ADR-0018, its safeguards). Earful's footer on every
// page of a survey carries a link to report it: a mailto: to the
// instance's published contact, with the survey's public address in the
// message. It is a link and not a form, so it stores nothing and gives a
// flood nothing to write; the operator answers from their own mail. An
// instance that publishes no contact shows no link, rather than one that
// goes nowhere.
//
// The address in the message is always the share link, /s/{id}: a
// personal invitation's address is its participant's credential, and is
// never put in a message somebody else will read.

// publicSurvey finds a survey by its id for the respondent's path, as
// the store does, and fills in the report link of whatever page follows.
func (s *server) publicSurvey(r *http.Request, surveyID uuid.UUID) (store.PublicSurvey, error) {
	survey, err := s.surveys.PublicSurvey(r.Context(), surveyID)
	if err == nil {
		if report := templates.ReportFrom(r.Context()); report != nil {
			report.Href = reportMailto(text(r), operatorContact(s.cfg), s.cfg.BaseURL+"/s/"+survey.ID.String())
		}
	}
	return survey, err
}

// operatorContact is where a report of a survey, and a creator's
// question about their suspended workspace, are written to: CONTACT_EMAIL
// and nothing else (ADR-0018). Unlike the documents' contact, it does not
// fall back to EMAIL_FROM, which on many instances is an address mail is
// sent from and nobody reads; a report sent there would be lost while
// the reporter believed it made. With no CONTACT_EMAIL, the link and the
// line are not shown.
func operatorContact(cfg config.Config) string { return cfg.ContactEmail }

// reportMailto is the report link for a survey at address, or "" when
// there is nobody to write to. Subject and body are encoded for a
// mailto: URL (RFC 6068), where a space is %20 and never a plus.
func reportMailto(l uitext.Localizer, contact, address string) string {
	if contact == "" {
		return ""
	}
	encode := func(s string) string {
		return strings.ReplaceAll(url.QueryEscape(s), "+", "%20")
	}
	subject := l.T("respond.report.subject")
	body := l.T("respond.report.body", uitext.Args{"Address": address})
	return "mailto:" + (&url.URL{Path: contact}).EscapedPath() +
		"?subject=" + encode(subject) + "&body=" + encode(body)
}
