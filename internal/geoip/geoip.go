// Package geoip resolves an IP address to a country code, in-process.
//
// ADR-0009 allows exactly one geographic fact — the country an answer
// came from, as a survey-level counter — and forbids gaining a processor
// to learn it. So the whole database ships inside the binary: no lookup
// service, no request leaving the machine, nothing to disclose on the
// trust page beyond the data's own attribution.
//
// The IP itself is discarded in the same request that resolves it. This
// package holds no state about who asked.
//
// Data: DB-IP IP-to-Country Lite, CC-BY 4.0 — see README.md for the
// attribution requirement and the refresh procedure.
package geoip

import (
	"bytes"
	"compress/gzip"
	"encoding/binary"
	"errors"
	"io"
	"net/netip"
	"sort"
	"sync"

	_ "embed"
)

//go:embed ranges.bin.gz
var packed []byte

var (
	once      sync.Once
	loadErr   error
	countries []string
	v4Starts  []uint32
	v4Country []uint16
	v6Starts  []uint64
	v6Country []uint16
)

// Country returns the ISO 3166-1 alpha-2 code for addr, and false when
// the address is private, unknown or unmapped. A caller that gets false
// records nothing rather than guessing.
func Country(addr netip.Addr) (string, bool) {
	if !addr.IsValid() || addr.IsLoopback() || addr.IsPrivate() ||
		addr.IsLinkLocalUnicast() || addr.IsUnspecified() {
		return "", false
	}
	once.Do(load)
	if loadErr != nil {
		return "", false
	}

	if addr.Is4() || addr.Is4In6() {
		v4 := addr.As4()
		key := binary.BigEndian.Uint32(v4[:])
		index := sort.Search(len(v4Starts), func(i int) bool { return v4Starts[i] > key }) - 1
		if index < 0 {
			return "", false
		}
		return code(v4Country[index])
	}

	v6 := addr.As16()
	key := binary.BigEndian.Uint64(v6[:8])
	index := sort.Search(len(v6Starts), func(i int) bool { return v6Starts[i] > key }) - 1
	if index < 0 {
		return "", false
	}
	return code(v6Country[index])
}

// code turns a table index into a country, refusing the "unknown"
// placeholder the source data uses.
func code(index uint16) (string, bool) {
	if int(index) >= len(countries) {
		return "", false
	}
	country := countries[index]
	if country == "ZZ" || country == "" {
		return "", false
	}
	return country, true
}

// Loaded reports whether the table is present and usable — the trust
// page and the operator's checks would rather say "country data is
// missing" than silently report no countries.
func Loaded() bool {
	once.Do(load)
	return loadErr == nil && len(v4Starts) > 0
}

func load() {
	reader, err := gzip.NewReader(bytes.NewReader(packed))
	if err != nil {
		loadErr = err
		return
	}
	defer reader.Close()
	raw, err := io.ReadAll(reader)
	if err != nil {
		loadErr = err
		return
	}
	loadErr = decode(raw)
}

func decode(raw []byte) error {
	const header = "EARGEO1\n"
	if len(raw) < len(header) || string(raw[:len(header)]) != header {
		return errors.New("geoip: not an Earful country table")
	}
	pos := len(header)

	take := func(n int) ([]byte, error) {
		if pos+n > len(raw) {
			return nil, errors.New("geoip: truncated country table")
		}
		out := raw[pos : pos+n]
		pos += n
		return out, nil
	}

	head, err := take(2)
	if err != nil {
		return err
	}
	count := int(binary.LittleEndian.Uint16(head))
	countries = make([]string, 0, count)
	for i := 0; i < count; i++ {
		code, err := take(2)
		if err != nil {
			return err
		}
		countries = append(countries, string(code))
	}

	head, err = take(4)
	if err != nil {
		return err
	}
	n := int(binary.LittleEndian.Uint32(head))
	v4Starts = make([]uint32, n)
	v4Country = make([]uint16, n)
	for i := 0; i < n; i++ {
		row, err := take(6)
		if err != nil {
			return err
		}
		v4Starts[i] = binary.LittleEndian.Uint32(row[:4])
		v4Country[i] = binary.LittleEndian.Uint16(row[4:])
	}

	head, err = take(4)
	if err != nil {
		return err
	}
	n = int(binary.LittleEndian.Uint32(head))
	v6Starts = make([]uint64, n)
	v6Country = make([]uint16, n)
	for i := 0; i < n; i++ {
		row, err := take(10)
		if err != nil {
			return err
		}
		v6Starts[i] = binary.LittleEndian.Uint64(row[:8])
		v6Country[i] = binary.LittleEndian.Uint16(row[8:])
	}
	return nil
}

// Attribution is the credit the CC-BY licence requires wherever the data
// is used. The trust page renders it.
const Attribution = "IP geolocation by DB-IP (IP-to-Country Lite, CC BY 4.0)"

// AttributionURL is the link that credit must carry.
const AttributionURL = "https://db-ip.com"
