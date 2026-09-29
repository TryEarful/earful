TEMPL_VERSION := v0.3.1020
GOOSE_VERSION := v3.24.1
# staticcheck/govulncheck deliberately float to @latest rather than a pin:
# older pinned builds (e.g. staticcheck 2024.1.1, govulncheck v1.1.4)
# produced corrupt Mach-O binaries (missing LC_UUID, refuse to run) when
# compiled by the newer Go toolchain this module requires (go.mod needs
# go >= 1.25.0, forced by a-h/templ); @latest builds cleanly. govulncheck
# also wants a current vulnerability DB more than build reproducibility.

# Deliberately does not set/override GOBIN: `go install` already resolves
# it (env GOBIN, else GOPATH/bin) and that directory is expected to be on
# PATH already (toolchain managers like mise put it there; CI adds it
# explicitly). Redefining GOBIN here previously shadowed an
# environment-provided GOBIN for every recipe in this file -- GNU Make
# re-exports environment-origin variables using their makefile-overridden
# value -- which silently redirected `go install` to a different directory
# than the one already on PATH.
PATH := $(PATH):$(shell go env GOPATH)/bin
export PATH

TEST_DATABASE_URL ?= postgres://earful:earful@localhost:5433/earful_test?sslmode=disable

.PHONY: tools generate generate-check dev build check test e2e-smoke featuretour featuretour-deck migrate purge geoip text-status text-accept compose-up compose-up-app compose-down docker-build release-check

tools:
	go install github.com/a-h/templ/cmd/templ@$(TEMPL_VERSION)
	go install github.com/pressly/goose/v3/cmd/goose@$(GOOSE_VERSION)
	go install honnef.co/go/tools/cmd/staticcheck@latest
	go install golang.org/x/vuln/cmd/govulncheck@latest
	@if [ "$$(uname)" = "Darwin" ]; then \
	  for bin in templ goose staticcheck govulncheck; do \
	    p=$$(command -v $$bin 2>/dev/null) && codesign -s - -f "$$p" >/dev/null 2>&1; \
	  done; \
	fi
	@command -v sqlc >/dev/null || { \
	  echo "sqlc not found. macOS: brew install sqlc"; \
	  echo "Other platforms: https://docs.sqlc.dev/en/stable/overview/install.html"; \
	  echo "(do NOT go install sqlc -- its cgo-heavy embedded PG parser can fail to build; see docs/testing.md)"; \
	  exit 1; \
	}

generate:
	templ generate
	sqlc generate

# Rebuild the embedded country table from a DB-IP IP-to-Country Lite CSV.
# Run it monthly-ish; see internal/geoip/README.md for the download and
# the CC-BY attribution the data carries.
GEOIP_CSV ?= /tmp/dbip-country-lite.csv
geoip:
	go run ./tools/geoipgen -in $(GEOIP_CSV)

# Translations in web/text made from English that has since been
# reworded. Reports, never fails. After reading them against the new
# wording: make text-accept, or make text-accept ID="a.b.c d.e.f".
text-status:
	go run ./tools/textstatus

text-accept:
	go run ./tools/textstatus -accept $(ID)

dev:
	go run ./cmd/earful serve

build:
	CGO_ENABLED=0 go build -o bin/earful ./cmd/earful

check: tools generate-check
	@unformatted=$$(gofmt -l $$(git ls-files '*.go' | grep -v '_templ.go')); \
	  if [ -n "$$unformatted" ]; then \
	    echo "gofmt: these files are not formatted:"; echo "$$unformatted"; \
	    echo "run: gofmt -w <file>"; exit 1; \
	  fi
	go vet ./...
	staticcheck ./...
	govulncheck ./...
	sqlc diff
	$(MAKE) test

# generate-check fails when committed generated output is stale -- i.e.
# someone edited a .templ (or db/queries) without regenerating. It
# compares content hashes taken either side of a regeneration rather than
# using `git diff`, because a dirty working tree is the normal state
# during development: freshly regenerated output legitimately differs from
# HEAD until the commit lands, and a git-based check cannot tell that
# apart from genuinely stale output. sqlc drift is covered by `sqlc diff`
# in `check` above, which reports without writing.
generate-check:
	@before=$$(find . -name '*_templ.go' | sort | xargs shasum | shasum); \
	templ generate; \
	after=$$(find . -name '*_templ.go' | sort | xargs shasum | shasum); \
	if [ "$$before" != "$$after" ]; then \
	  echo "templ output was out of date; it has now been regenerated -- review and commit the *_templ.go changes"; \
	  exit 1; \
	fi

test: tools
	docker compose up -d --wait postgres
	TEST_DATABASE_URL=$(TEST_DATABASE_URL) go test ./...

migrate:
	go run ./cmd/earful migrate

purge:
	go run ./cmd/earful purge --dry-run

# Playwright + axe suite against the real compose stack (M4-T1). The app
# restart resets in-memory rate limiters, so repeated local runs don't
# inherit a spent magic-link budget from earlier manual testing.
e2e-smoke:
	docker compose --profile app up -d --build --wait app mailpit
	docker compose --profile app restart app
	cd e2e && npm install && npx playwright install chromium && npx playwright test

# The feature-tour deck (tools/featuretour): a PDF walkthrough of the
# product built from screenshots of this compose stack. The image is
# rebuilt here so the deck shows the working tree; the tool itself only
# recreates the app container with the AI environment it needs.
featuretour:
	docker compose --profile app up -d --build --wait app mailpit
	cd tools/featuretour && deno task run

# Rebuild only the PDF from the screenshots of the last run, for edits
# to tools/featuretour/content/deck.md.
featuretour-deck:
	cd tools/featuretour && deno task deck

# Supporting services only (Postgres, mailpit), for an app run with `make dev`.
compose-up:
	docker compose up -d --wait

# The whole stack in containers, app included: the self-hosting path.
compose-up-app:
	docker compose --profile app up --build

# --profile app so the app and migrate containers are removed as well; down
# only touches services whose profile is enabled.
compose-down:
	docker compose --profile app down -v

docker-build:
	docker build -t earful:local .

# The pipeline moves only the image; OpenTofu owns the environment. A
# release whose code expects a new environment value (a model id, the
# Vertex location, a budget) is only complete once both have landed, so
# this refuses to let a tag go out while either env still has an
# unapplied plan. Exit code 2 from -detailed-exitcode means "changes
# pending"; 1 is an error (usually an env that was never `tofu init`ed,
# see deploy/opentofu/README.md).
release-check:
	@for env in stg pro; do \
		echo "release-check: tofu plan envs/$$env"; \
		(cd deploy/opentofu/envs/$$env && tofu plan -detailed-exitcode -input=false -lock=false >/dev/null); \
		case $$? in \
			0) echo "release-check: envs/$$env is applied" ;; \
			2) echo "release-check: unapplied tofu changes in envs/$$env; apply before tagging" >&2; exit 1 ;; \
			*) echo "release-check: tofu plan failed in envs/$$env" >&2; exit 1 ;; \
		esac; \
	done
