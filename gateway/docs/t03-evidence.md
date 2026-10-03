# T03 catalog slice — 2026-10-03

Implemented GET toolkits, toolkit detail and auth_configs using project-scoped
PostgreSQL records. Unconfigured, empty/NULL/wildcard Action admission is
excluded. Disabled configurations require show_disabled=true and remain
DISABLED. Custom OAuth is never labeled Composio-managed. One enabled config
per project/toolkit is enforced by a concurrent unique index.

The real Multica service requests sort_by=usage. This profile ranks by local
successful executions (not Composio global popularity), with stable slug ties.
Alphabetical ordering uses display name and slug. Category metadata is not
implemented; a nonempty category filter returns 501 rather than being ignored.

Cursors bind project, route, complete filter set and a digest of the current
catalog including usage. Configuration or usage changes invalidate an old
cursor explicitly instead of silently skipping reordered entries. Unknown,
duplicate and malformed query fields are rejected before processing.

## Evidence

`tests/integration/catalog_test.go` runs the real Multica SDK against the
real gateway handler and PostgreSQL 17.11. It verifies catalog parsing,
custom auth configs, disabled and unconfigured records, project boundaries,
cursor tampering, changed filters, usage drift, actual usage ordering and
unsupported revoke errors. Provider capability flags are synthetic fixtures;
no real native provider configuration was verified.

Full `go test -race -count=1 ./...` with the isolated PostgreSQL test URL:
PASS after fixes. `npm run fix-check`: PASS before the final Go-only fixes;
formatting and Go checks were repeated for those changes.

Independent reviewer `/root/review_t02` found a P2: live usage could move a
record ahead of a keyset cursor and silently lose it. Binding the signed
cursor to a catalog/usage digest fixed that; a real SQL execution insertion
now tests invalidation and the new ranking.

The first cross-package race run also found a migration deadlock: a blocking
advisory-lock query retained a snapshot that concurrent index creation needed
to drain. Migration locking now uses pg_try_advisory_lock, finishing each
query before waiting outside PostgreSQL. Unlock is bounded and failed unlock
closes the connection. The full cross-package race suite subsequently passed.

C01 and C06 have SDK/gateway/PostgreSQL evidence. C02/C03/C04 are partial
(catalog only); account listing, native provider verification, OAuth and MCP
remain unimplemented. This does not establish P1A or P1B completion.
