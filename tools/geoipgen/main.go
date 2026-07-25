// Command geoipgen turns the DB-IP IP-to-Country Lite CSV into the
// compact table internal/geoip embeds.
//
// The CSV is 25 MB of `start,end,country` lines, most of them adjacent
// ranges with the same country. Merging those and storing only each
// range's start (the next range's start being the implicit end) gets the
// whole world into a file small enough to ship inside the binary — which
// is the point: country lookup happens in-process, so no request ever
// leaves for a geolocation service and no new processor appears on the
// trust page (ADR-0009).
//
//	make geoip   # after downloading the CSV; see internal/geoip/README.md
package main

import (
	"bytes"
	"compress/gzip"
	"encoding/binary"
	"encoding/csv"
	"flag"
	"fmt"
	"io"
	"net/netip"
	"os"
	"sort"
)

func main() {
	in := flag.String("in", "", "DB-IP IP-to-Country Lite CSV")
	out := flag.String("out", "internal/geoip/ranges.bin.gz", "generated table")
	flag.Parse()
	if *in == "" {
		fmt.Fprintln(os.Stderr, "usage: geoipgen -in dbip-country-lite.csv [-out internal/geoip/ranges.bin.gz]")
		os.Exit(2)
	}
	if err := run(*in, *out); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

type v4Range struct {
	start   uint32
	country uint16
}

type v6Range struct {
	// prefix is the top 64 bits of the address. Every real-world country
	// allocation is far larger than a /64, so this loses nothing and
	// halves the table.
	prefix  uint64
	country uint16
}

func run(inPath, outPath string) error {
	file, err := os.Open(inPath)
	if err != nil {
		return err
	}
	defer file.Close()

	reader := csv.NewReader(file)
	reader.FieldsPerRecord = 3
	reader.ReuseRecord = true

	countries := map[string]uint16{}
	var countryList []string
	index := func(code string) uint16 {
		if i, ok := countries[code]; ok {
			return i
		}
		i := uint16(len(countryList))
		countries[code] = i
		countryList = append(countryList, code)
		return i
	}

	var v4 []v4Range
	var v6 []v6Range
	for {
		record, err := reader.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("read csv: %w", err)
		}
		start, err := netip.ParseAddr(record[0])
		if err != nil {
			continue
		}
		code := record[2]
		if len(code) != 2 {
			continue
		}
		country := index(code)
		if start.Is4() {
			v4 = append(v4, v4Range{start: binary.BigEndian.Uint32(start.AsSlice()), country: country})
			continue
		}
		bytes := start.As16()
		v6 = append(v6, v6Range{prefix: binary.BigEndian.Uint64(bytes[:8]), country: country})
	}

	sort.Slice(v4, func(i, j int) bool { return v4[i].start < v4[j].start })
	sort.Slice(v6, func(i, j int) bool { return v6[i].prefix < v6[j].prefix })
	v4 = mergeV4(v4)
	v6 = mergeV6(v6)

	if len(countryList) > 1<<16 {
		return fmt.Errorf("too many country codes for a uint16 index: %d", len(countryList))
	}

	var buf []byte
	buf = append(buf, "EARGEO1\n"...)
	buf = binary.LittleEndian.AppendUint16(buf, uint16(len(countryList)))
	for _, code := range countryList {
		buf = append(buf, code[0], code[1])
	}
	buf = binary.LittleEndian.AppendUint32(buf, uint32(len(v4)))
	for _, r := range v4 {
		buf = binary.LittleEndian.AppendUint32(buf, r.start)
		buf = binary.LittleEndian.AppendUint16(buf, r.country)
	}
	buf = binary.LittleEndian.AppendUint32(buf, uint32(len(v6)))
	for _, r := range v6 {
		buf = binary.LittleEndian.AppendUint64(buf, r.prefix)
		buf = binary.LittleEndian.AppendUint16(buf, r.country)
	}

	// Gzipped in the repo and in the binary: the table is 5 MB of sorted
	// numbers, which compresses to under 2 MB, and it is decompressed
	// once on first lookup.
	var compressed bytes.Buffer
	writer, err := gzip.NewWriterLevel(&compressed, gzip.BestCompression)
	if err != nil {
		return err
	}
	if _, err := writer.Write(buf); err != nil {
		return err
	}
	if err := writer.Close(); err != nil {
		return err
	}
	if err := os.WriteFile(outPath, compressed.Bytes(), 0o644); err != nil {
		return err
	}
	fmt.Printf("%s: %d countries, %d IPv4 ranges, %d IPv6 ranges, %.1f MB (%.1f MB uncompressed)\n",
		outPath, len(countryList), len(v4), len(v6),
		float64(compressed.Len())/(1<<20), float64(len(buf))/(1<<20))
	return nil
}

// mergeV4 collapses consecutive ranges that resolve to the same country,
// which is most of them.
func mergeV4(in []v4Range) []v4Range {
	out := in[:0]
	var last uint16 = 1<<16 - 1
	for _, r := range in {
		if r.country == last {
			continue
		}
		out = append(out, r)
		last = r.country
	}
	return out
}

func mergeV6(in []v6Range) []v6Range {
	out := in[:0]
	var last uint16 = 1<<16 - 1
	var lastPrefix uint64
	for i, r := range in {
		if i > 0 && (r.country == last || r.prefix == lastPrefix) {
			continue
		}
		out = append(out, r)
		last, lastPrefix = r.country, r.prefix
	}
	return out
}
