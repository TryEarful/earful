package geoip_test

import (
	"net/netip"
	"testing"

	"github.com/TryEarful/earful/internal/geoip"
)

func TestCountry_ResolvesWellKnownAddresses(t *testing.T) {
	t.Parallel()
	if !geoip.Loaded() {
		t.Fatal("the embedded country table did not load")
	}
	// Anchors that have not moved in years. Exact values can drift with
	// the monthly refresh — and the free dataset is coarse about anycast
	// (Google's public DNS reads as CA here, not US) — so this pins
	// stable allocations rather than famous addresses.
	cases := []struct {
		addr string
		want string
	}{
		{"8.8.8.8", "US"},       // Google, IPv4
		{"1.1.1.1", "AU"},       // Cloudflare, registered in AU
		{"81.169.145.68", "DE"}, // Strato, Berlin
	}
	for _, tc := range cases {
		addr := netip.MustParseAddr(tc.addr)
		got, ok := geoip.Country(addr)
		if !ok {
			t.Errorf("Country(%s) reported unknown", tc.addr)
			continue
		}
		if got != tc.want {
			t.Errorf("Country(%s) = %q, want %q (check the data refresh)", tc.addr, got, tc.want)
		}
	}

	// IPv6 resolves too — to a plausible code, whatever the data says
	// about any particular anycast address this month.
	v6, ok := geoip.Country(netip.MustParseAddr("2a00:1450:4001:800::200e")) // Google, EU
	if !ok || len(v6) != 2 {
		t.Errorf("IPv6 lookup = %q (%v), want a two-letter country", v6, ok)
	}
}

// TestCountry_RefusesWhatItCannotKnow: a private or loopback address has
// no country, and the caller must record nothing rather than guess.
func TestCountry_RefusesWhatItCannotKnow(t *testing.T) {
	t.Parallel()
	for _, raw := range []string{"127.0.0.1", "10.0.0.1", "192.168.1.20", "::1", "fe80::1", "0.0.0.0"} {
		if country, ok := geoip.Country(netip.MustParseAddr(raw)); ok {
			t.Errorf("Country(%s) = %q, want unknown", raw, country)
		}
	}
	var invalid netip.Addr
	if _, ok := geoip.Country(invalid); ok {
		t.Error("an invalid address resolved to a country")
	}
}

func BenchmarkCountry(b *testing.B) {
	addr := netip.MustParseAddr("81.169.145.68")
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		geoip.Country(addr)
	}
}
