package http

import (
	"net/http"
	"strings"
	"time"

	"github.com/TryEarful/earful/internal/uitext"
	"github.com/TryEarful/earful/web/templates"
)

// The language a request is answered in (ADR-0014).
//
// A respondent's is read from the address and the browser, and from
// nothing else: what language a person reads is a fact about them, and a
// respondent is promised that nothing about them is kept (story 25,
// ADR-0003). Anyone else may choose a language and have the choice
// remembered, in a cookie.

// languageCookie holds the language chosen with the switcher.
const languageCookie = "interface_lang"

// say renders a message in the language the request is answered in.
// Handlers word what they show through it, as templates do through t.
func say(r *http.Request, id uitext.ID, args ...uitext.Args) string {
	return uitext.From(r.Context()).T(id, args...)
}

// sayN renders the form of a message that suits count.
func sayN(r *http.Request, id uitext.ID, count int, args ...uitext.Args) string {
	return uitext.From(r.Context()).N(id, count, args...)
}

// text is the wording a request is answered in, for a function that
// words several things or is handed what it needs rather than a request.
func text(r *http.Request) uitext.Localizer {
	return uitext.From(r.Context())
}

// interfaceLanguage is the language a request is answered in.
func interfaceLanguage(r *http.Request) string {
	return uitext.From(r.Context()).Lang()
}

// reading is who a page is for, as far as choosing its language goes.
type reading int

const (
	// everyone is a creator, or a visitor to a page that is not a
	// survey: the switcher's cookie, then the browser.
	everyone reading = iota
	// respondent is somebody answering: the language of the survey
	// they chose, then the browser. The cookie is not read.
	respondent
	// reader is somebody reading a document, who may have come from a
	// survey and may be a creator: the address, the cookie, the browser.
	reader
)

func (s *server) audienceOf(path string) reading {
	if strings.HasPrefix(path, "/s/") || strings.HasPrefix(path, "/p/") {
		return respondent
	}
	address := strings.TrimSuffix(strings.TrimPrefix(path, "/"), ".md")
	if _, ok := s.pages.Page(address, uitext.Source); ok {
		return reader
	}
	return everyone
}

// localizerFor chooses the language of a request. What is read is read
// in order, and what is not a language the interface is written in is
// passed over for the next.
//
// A creator's page does not read ?lang= at all: on the results page it
// is the language answers are translated into, which is a different
// question from what language the page is in.
func (s *server) localizerFor(r *http.Request) uitext.Localizer {
	browser := r.Header.Get("Accept-Language")
	chosen := ""
	if c, err := r.Cookie(languageCookie); err == nil {
		chosen = c.Value
	}
	switch s.audienceOf(r.URL.Path) {
	case respondent:
		return s.text.Localizer(r.URL.Query().Get("lang"), browser)
	case reader:
		return s.text.Localizer(r.URL.Query().Get("lang"), chosen, browser)
	default:
		return s.text.Localizer(chosen, browser)
	}
}

// interfaceText gives every request the wording it is answered in.
// Templates and handlers read it from the context, so a page is worded
// in one language throughout without any of them being told which.
func (s *server) interfaceText(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := uitext.With(r.Context(), s.localizerFor(r))
		// Where the switcher comes back to.
		ctx = templates.WithPath(ctx, r.URL.RequestURI())
		ctx = templates.WithMode(ctx, modeOf(r))
		// Filled in by publicSurvey on a survey's pages; see report.go.
		ctx = templates.WithReport(ctx)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// chooseLanguage is the switcher: it remembers the language chosen and
// goes back to the page the choice was made on. It is a form and a
// redirect, so it works without a script.
//
// It needs no session, because the sign-in page has a switcher too, and
// no token of its own: the cross-origin check in front of every handler
// refuses a form posted from elsewhere, and the worst a forged choice
// could do is show somebody their own pages in another language.
func (s *server) chooseLanguage(w http.ResponseWriter, r *http.Request) {
	if lang := r.PostFormValue("lang"); s.text.Serves(lang) {
		http.SetCookie(w, &http.Cookie{
			Name:     languageCookie,
			Value:    lang,
			Path:     "/",
			Expires:  s.clock.Now().Add(365 * 24 * time.Hour),
			HttpOnly: true,
			Secure:   s.cfg.SecureCookies(),
			SameSite: http.SameSiteLaxMode,
		})
	}
	http.Redirect(w, r, localPath(r.PostFormValue("next")), http.StatusSeeOther)
}

// sitePath is next if it is a path on this site, and the home page if
// it is anything else. A redirect to wherever a form says would send a
// person to any site that could get them to press a button here.
func sitePath(next string) string {
	if !strings.HasPrefix(next, "/") || strings.HasPrefix(next, "//") || strings.ContainsAny(next, "\\\r\n") {
		return "/"
	}
	return next
}

// localPath is where the language switcher comes back to: a path on
// this site, as sitePath, and never a survey.
func localPath(next string) string {
	next = sitePath(next)
	// A survey is answered in the language its address names and the
	// switcher is not shown there, so nothing sends anybody back to one.
	if strings.HasPrefix(next, "/s/") || strings.HasPrefix(next, "/p/") {
		return "/"
	}
	return next
}
