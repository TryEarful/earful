package http

import "testing"

// A redirect to wherever a form says would send a person to any site
// that could get them to press a button on this one.
func TestTheSwitcherComesBackToThisSite(t *testing.T) {
	for next, want := range map[string]string{
		"/dashboard":                   "/dashboard",
		"/surveys/abc/results?lang=en": "/surveys/abc/results?lang=en",
		"/login?notice=signed_out":     "/login?notice=signed_out",
		"":                             "/",
		"dashboard":                    "/",
		"//evil.example/":              "/",
		"https://evil.example/":        "/",
		"/\\evil.example":              "/",
		"/dashboard\r\nSet-Cookie: x":  "/",
		// Nothing sends anybody to a survey: it has no switcher, and its
		// language is in its address.
		"/s/0b0e4d2e-0000-4000-8000-000000000000": "/",
		"/p/some-token": "/",
	} {
		if got := localPath(next); got != want {
			t.Errorf("%q → %q, want %q", next, got, want)
		}
	}
}
