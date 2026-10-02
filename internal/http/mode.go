package http

import (
	"net/http"
	"time"
)

// The display mode a page is drawn in: light, dark, or, with nothing
// chosen, whatever the reader's system asks for (docs/style-guide.md).
//
// The choice is kept in a cookie and drawn by the server, as
// data-mode on <html>, so the page arrives in its mode: a script that
// read the choice from browser storage would have to run inline before
// the stylesheet, which the Content-Security-Policy refuses (ADR-0006),
// and would leave a reader without JavaScript no way to choose.
//
// A respondent may choose too. The cookie is set only when they do,
// holds one of two words and nothing about them, and is read only to
// draw the page.

// modeCookie holds the display mode chosen with the switcher.
const modeCookie = "mode"

// earlierModeCookie is the name the choice was kept under before
// "theme" came to mean the look a creator gives a survey. It lasts a
// year in a browser, so it is still read where modeCookie is absent,
// and removed whenever a choice is made.
const earlierModeCookie = "theme"

// modes are the values the cookie may hold. Following the system is
// the absence of the cookie.
var modes = map[string]bool{"light": true, "dark": true}

// modeOf is the display mode a request chose, or "" to follow the
// system. Anything else in the cookie is passed over.
func modeOf(r *http.Request) string {
	for _, name := range []string{modeCookie, earlierModeCookie} {
		if c, err := r.Cookie(name); err == nil && modes[c.Value] {
			return c.Value
		}
	}
	return ""
}

// chooseMode is the display mode switcher: it remembers the mode
// chosen, or forgets it for "system", and goes back to the page the
// choice was made on. A value it does not know changes nothing.
//
// Like the language switcher it needs no session and no token: the
// cross-origin check in front of every handler refuses a form posted
// from elsewhere, and a forged choice could do no more than recolour
// somebody's pages. Unlike it, it comes back to a survey, since a
// respondent's page has one.
func (s *server) chooseMode(w http.ResponseWriter, r *http.Request) {
	forget := func(name string) {
		http.SetCookie(w, &http.Cookie{
			Name:     name,
			Value:    "",
			Path:     "/",
			MaxAge:   -1,
			HttpOnly: true,
			Secure:   s.cfg.SecureCookies(),
			SameSite: http.SameSiteLaxMode,
		})
	}
	switch mode := r.PostFormValue("mode"); {
	case modes[mode]:
		http.SetCookie(w, &http.Cookie{
			Name:     modeCookie,
			Value:    mode,
			Path:     "/",
			Expires:  s.clock.Now().Add(365 * 24 * time.Hour),
			HttpOnly: true,
			Secure:   s.cfg.SecureCookies(),
			SameSite: http.SameSiteLaxMode,
		})
		if _, err := r.Cookie(earlierModeCookie); err == nil {
			forget(earlierModeCookie)
		}
	case mode == "system":
		forget(modeCookie)
		if _, err := r.Cookie(earlierModeCookie); err == nil {
			forget(earlierModeCookie)
		}
	}
	http.Redirect(w, r, sitePath(r.PostFormValue("next")), http.StatusSeeOther)
}
