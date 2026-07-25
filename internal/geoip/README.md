# Country lookup data

`ranges.bin.gz` is generated from the **DB-IP IP-to-Country Lite**
database and embedded in the binary, so resolving a country never leaves
the process — no lookup service, no new sub-processor, nothing extra to
disclose (ADR-0009).

## Licence and attribution

DB-IP IP-to-Country Lite is published under **CC BY 4.0**, which requires
visible credit wherever the data is used. That credit is
`geoip.Attribution`, rendered on `/trust`. Do not remove it, and keep it
in place if the data source changes to another CC-BY database.

The source CSV is deliberately *not* committed (25 MB); the generated
table is, so a build needs no download.

## Refreshing (monthly-ish)

```sh
curl -sSL -o /tmp/dbip.csv.gz \
  https://download.db-ip.com/free/dbip-country-lite-$(date +%Y-%m).csv.gz
gunzip -f /tmp/dbip.csv.gz
make geoip GEOIP_CSV=/tmp/dbip.csv
```

`make geoip` runs `tools/geoipgen`, which merges adjacent ranges of the
same country, keeps only each range's start (the next start is the
implicit end), and gzips the result — 25 MB of CSV becomes about 1.7 MB
in the repository and roughly 5 MB of memory after the first lookup.

Country data drifts slowly; a stale table misattributes a small number of
addresses to a neighbouring allocation. That is a known and acceptable
inaccuracy for a suppressed, survey-level counter, and a reason not to
present these numbers as precise.
