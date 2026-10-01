package http

import (
	"net/http"
	"time"
)

// The theme a page is drawn in: light, dark, or, with nothing chosen,
// whatever the reader's system asks for (docs/style-guide.md).
//
// The choice is kept in a cookie and drawn by the server, as
// data-theme on <html>, so the page arrives in its theme: a script that
// read the choice from browser storage would have to run inline before
// the stylesheet, which the Content-Security-Policy refuses (ADR-0006),
// and would leave a reader without JavaScript no way to choose.
//
// A respondent may choose too. The cookie is set only when they do,
// holds one of two words and nothing about them, and is read only to
// draw the page.

// themeCookie holds the theme chosen with the switcher.
const themeCookie = "theme"

// themes are the values the cookie may hold. Following the system is
// the absence of the cookie.
var themes = map[string]bool{"light": true, "dark": true}

// themeOf is the theme a request chose, or "" to follow the system.
// Anything else in the cookie is passed over.
func themeOf(r *http.Request) string {
	if c, err := r.Cookie(themeCookie); err == nil && themes[c.Value] {
		return c.Value
	}
	return ""
}

// chooseTheme is the theme switcher: it remembers the theme chosen, or
// forgets it for "system", and goes back to the page the choice was made
// on. A value it does not know changes nothing.
//
// Like the language switcher it needs no session and no token: the
// cross-origin check in front of every handler refuses a form posted
// from elsewhere, and a forged choice could do no more than recolour
// somebody's pages. Unlike it, it comes back to a survey, since a
// respondent's page has one.
func (s *server) chooseTheme(w http.ResponseWriter, r *http.Request) {
	switch theme := r.PostFormValue("theme"); {
	case themes[theme]:
		http.SetCookie(w, &http.Cookie{
			Name:     themeCookie,
			Value:    theme,
			Path:     "/",
			Expires:  s.clock.Now().Add(365 * 24 * time.Hour),
			HttpOnly: true,
			Secure:   s.cfg.SecureCookies(),
			SameSite: http.SameSiteLaxMode,
		})
	case theme == "system":
		http.SetCookie(w, &http.Cookie{
			Name:     themeCookie,
			Value:    "",
			Path:     "/",
			MaxAge:   -1,
			HttpOnly: true,
			Secure:   s.cfg.SecureCookies(),
			SameSite: http.SameSiteLaxMode,
		})
	}
	http.Redirect(w, r, sitePath(r.PostFormValue("next")), http.StatusSeeOther)
}
