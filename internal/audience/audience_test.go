package audience_test

import (
	"testing"

	"github.com/TryEarful/earful/internal/audience"
)

// Real user-agent strings, because the only thing that matters here is
// what actual browsers send. Order is the trap: every Chromium browser
// claims to be Chrome and Safari, and most tablets claim to be mobile.
func TestBrowserFamilyAndDeviceClass(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name    string
		ua      string
		browser string
		device  string
	}{
		{
			name:    "Chrome on macOS",
			ua:      "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/141.0.0.0 Safari/537.36",
			browser: audience.BrowserChrome, device: audience.DeviceDesktop,
		},
		{
			name:    "Edge is not Chrome",
			ua:      "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/141.0.0.0 Safari/537.36 Edg/141.0.0.0",
			browser: audience.BrowserEdge, device: audience.DeviceDesktop,
		},
		{
			name:    "Opera is not Chrome",
			ua:      "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/140.0.0.0 Safari/537.36 OPR/125.0.0.0",
			browser: audience.BrowserOpera, device: audience.DeviceDesktop,
		},
		{
			name:    "Safari on iPhone",
			ua:      "Mozilla/5.0 (iPhone; CPU iPhone OS 17_0 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.0 Mobile/15E148 Safari/604.1",
			browser: audience.BrowserSafari, device: audience.DevicePhone,
		},
		{
			name:    "Chrome on iPhone is still Chrome",
			ua:      "Mozilla/5.0 (iPhone; CPU iPhone OS 17_0 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) CriOS/141.0.0.0 Mobile/15E148 Safari/604.1",
			browser: audience.BrowserChrome, device: audience.DevicePhone,
		},
		{
			name:    "iPad is a tablet",
			ua:      "Mozilla/5.0 (iPad; CPU OS 17_0 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.0 Mobile/15E148 Safari/604.1",
			browser: audience.BrowserSafari, device: audience.DeviceTablet,
		},
		{
			name:    "Android without Mobile is a tablet",
			ua:      "Mozilla/5.0 (Linux; Android 14; SM-X710) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/141.0.0.0 Safari/537.36",
			browser: audience.BrowserChrome, device: audience.DeviceTablet,
		},
		{
			name:    "Android with Mobile is a phone",
			ua:      "Mozilla/5.0 (Linux; Android 14; Pixel 8) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/141.0.0.0 Mobile Safari/537.36",
			browser: audience.BrowserChrome, device: audience.DevicePhone,
		},
		{
			name:    "Samsung Internet",
			ua:      "Mozilla/5.0 (Linux; Android 14; SM-S918B) AppleWebKit/537.36 (KHTML, like Gecko) SamsungBrowser/25.0 Chrome/135.0.0.0 Mobile Safari/537.36",
			browser: audience.BrowserSamsung, device: audience.DevicePhone,
		},
		{
			name:    "Firefox on Linux",
			ua:      "Mozilla/5.0 (X11; Linux x86_64; rv:130.0) Gecko/20100101 Firefox/130.0",
			browser: audience.BrowserFirefox, device: audience.DeviceDesktop,
		},
		{
			name:    "a bot or an empty header stays unclassified",
			ua:      "",
			browser: audience.BrowserOther, device: audience.DeviceOther,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := audience.BrowserFamily(tc.ua); got != tc.browser {
				t.Errorf("BrowserFamily = %q, want %q", got, tc.browser)
			}
			if got := audience.DeviceClass(tc.ua); got != tc.device {
				t.Errorf("DeviceClass = %q, want %q", got, tc.device)
			}
		})
	}
}

// TestBrowserFamilyKeepsNoVersion: a family is a fact worth knowing; a
// version string is a fingerprint (ADR-0009 keeps this coarse).
func TestBrowserFamilyKeepsNoVersion(t *testing.T) {
	t.Parallel()
	got := audience.BrowserFamily("Mozilla/5.0 ... Chrome/141.0.7390.55 Safari/537.36")
	if got != audience.BrowserChrome {
		t.Fatalf("BrowserFamily = %q", got)
	}
	for _, digit := range "0123456789" {
		for _, r := range got {
			if r == digit {
				t.Fatalf("browser family %q carries version detail", got)
			}
		}
	}
}

func TestSuppressed(t *testing.T) {
	t.Parallel()
	for count, want := range map[int]bool{0: true, 1: true, 4: true, 5: false, 50: false} {
		if got := audience.Suppressed(count); got != want {
			t.Errorf("Suppressed(%d) = %v, want %v", count, got, want)
		}
	}
}
