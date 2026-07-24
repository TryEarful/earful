# Migrations

goose migrations, numbered sequentially (`00001_...sql`, `00002_...sql`, ...).
Embedded into the binary via `migrations.go` and applied by `earful migrate`
(see `internal/store/migrate.go`).

Multi-statement migrations (trigger functions, needed starting M2/M3 for
version-immutability enforcement) must wrap the statement in
`-- +goose StatementBegin` / `-- +goose StatementEnd` so goose doesn't split
on semicolons inside the body. Single-statement migrations like `00001`
don't need it.

`sqlc.yaml` points its `schema:` at this directory directly -- sqlc parses
the DDL and ignores `-- +goose` comments as ordinary SQL comments.
