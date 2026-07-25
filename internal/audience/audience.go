// Package audience derives the three coarse facts ADR-0009 allows about
// who answered a survey: browser family, device class, and (via
// internal/geoip) country.
//
// Everything here is computed in the request and thrown away. The
// user-agent string is never stored, the IP is never stored, and what
// survives is a counter on a survey — never a column on a response. The
// blessed list is exhaustive: anything beyond these reopens the ADR.
package audience

import "strings"

// Browser families. Deliberately coarse: "Chrome" not "Chrome 141.0.3",
// because a version string is a fingerprint and a family is a fact worth
// knowing.
const (
	BrowserChrome  = "Chrome"
	BrowserSafari  = "Safari"
	BrowserFirefox = "Firefox"
	BrowserEdge    = "Edge"
	BrowserOpera   = "Opera"
	BrowserSamsung = "Samsung Internet"
	BrowserOther   = "Other"
)

// Device classes, equally coarse.
const (
	DevicePhone   = "Phone"
	DeviceTablet  = "Tablet"
	DeviceDesktop = "Desktop"
	DeviceOther   = "Other"
)

// BrowserFamily reads a family out of a user-agent string. Order
// matters: every Chromium browser claims to be Chrome and Safari, so the
// specific ones have to be recognised first.
func BrowserFamily(userAgent string) string {
	ua := userAgent
	switch {
	case ua == "":
		return BrowserOther
	case contains(ua, "Edg/"), contains(ua, "Edge/"), contains(ua, "EdgiOS"), contains(ua, "EdgA"):
		return BrowserEdge
	case contains(ua, "OPR/"), contains(ua, "Opera"):
		return BrowserOpera
	case contains(ua, "SamsungBrowser"):
		return BrowserSamsung
	case contains(ua, "Firefox/"), contains(ua, "FxiOS"):
		return BrowserFirefox
	case contains(ua, "Chrome/"), contains(ua, "CriOS"), contains(ua, "Chromium"):
		return BrowserChrome
	case contains(ua, "Safari/"):
		return BrowserSafari
	default:
		return BrowserOther
	}
}

// DeviceClass reads phone/tablet/desktop out of a user-agent string.
// Tablets are checked first because most of them also say "Mobile" or
// carry an Android token.
func DeviceClass(userAgent string) string {
	ua := userAgent
	switch {
	case ua == "":
		return DeviceOther
	case contains(ua, "iPad"), contains(ua, "Tablet"), contains(ua, "PlayBook"), contains(ua, "Silk"):
		return DeviceTablet
	// Android without "Mobile" is the standard tablet signal.
	case contains(ua, "Android") && !contains(ua, "Mobile"):
		return DeviceTablet
	case contains(ua, "iPhone"), contains(ua, "iPod"), contains(ua, "Mobile"),
		contains(ua, "Android"), contains(ua, "Windows Phone"):
		return DevicePhone
	case contains(ua, "Macintosh"), contains(ua, "Windows"), contains(ua, "X11"),
		contains(ua, "Linux"), contains(ua, "CrOS"):
		return DeviceDesktop
	default:
		return DeviceOther
	}
}

func contains(haystack, needle string) bool { return strings.Contains(haystack, needle) }

// SuppressBelow is ADR-0009's n < 5 rule: a bucket with fewer than five
// observations is not shown, so a small anonymous sample cannot single
// anybody out.
const SuppressBelow = 5

// Suppressed reports whether a bucket's count must be hidden.
func Suppressed(count int) bool { return count < SuppressBelow }
