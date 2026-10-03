# T02 foundation evidence — 2026-10-03

This is an incomplete foundation, not a usable provider integration. Control
and execution handlers are separate; authenticated unimplemented operations
return 501. The executable requires loopback listeners and explicit database
secret-file input. No credential/bootstrap or session mint endpoint exists.

Implemented: explicit migrations and readiness, project/subject/account and
session/grant records, digest-only bearer lookup, AES-GCM with project/record
AAD for upstream runtime secrets, current-policy admission without cache,
atomic session revocation plus outbox, and explicit recovery quarantine.
Outbox workers, provider configuration verification, OAuth, MCP execution,
Multica claim/lifecycle wiring and native OC patches are not implemented.

## Actual verification

Environment: Go 1.26.6, PostgreSQL 17.11 in this task's disposable local
`oc-recovery-pg` container. Image `postgres:17-alpine` resolved to
`sha256:b0f9560a2de083e2cc7382e75f808c7381a32852a7ec49117deedb300e552b24`.
Port 55432 is bound only to loopback. Fixture credentials are synthetic.
Every DB test creates and removes its own schema in `oc_gateway_test`.

Commands, with GOPATH/GOCACHE/GOMODCACHE scoped under
`/tmp/oc-recovery-tools` and `GOTOOLCHAIN=local`:

```sh
GATEWAY_TEST_DATABASE_URL='<isolated-local-fixture-url>' go test -race -count=1 ./...
go vet ./...
```

The first PostgreSQL run failed because a mixed-case generated schema was
unquoted in search_path. After quoting the identifier, DB checks passed.
The full foundation suite and race run then passed. Test evidence covers
current grant checks, expiry, disabled principals, cross-project revoke
rejection, revoke idempotence, atomic outbox, retained runtime ciphertext,
restore quarantine and failure-closed database outages. It does not prove
native revocation or daemon task termination.

OC native baseline checks: `node node_modules/vitest/vitest.mjs run
src/mcp.test.ts src/server/api/runtime-api.test.ts`: 57 tests passed.
`npm run fix-check`: passed.

## Independent read-only review

Reviewer task `/root/review_t02` found a P1 SQL three-valued-logic defect:
`NOT (value=ANY(array))` inside NOT EXISTS could admit a nonmatching grant
when an array contained NULL. All three checks were changed to
`(value=ANY(array)) IS NOT TRUE`; regression cases cover connection, agent
Action and provider Action arrays. The reviewer made no workspace changes.

The review also identified a recovery limitation: a running backup contains
`quarantined=false`. A restored database is NOT automatically detected.
Keep listeners stopped, run `-mode quarantine-restore`, rotate secrets and
reconcile dependencies before an explicitly approved reopening procedure.
The quarantine operation also invalidates pending OAuth transactions and
quarantines PREPARED executions; DISPATCHED executions become UNKNOWN.
It preserves ciphertext and blocks pending/leased outbox jobs.

A concurrent-index migration interrupted between index creation and ledger
insertion stops on retry. Serving remains closed while the ledger is
incomplete. An operator must inspect index validity/definition and complete
or rebuild that one index using the checked-in SQL; no automated repair or
production recovery procedure is claimed in this foundation.

All 40 end-to-end acceptance items remain NOT_RUN. Production, L3 provider
accounts, full daemon E2E and a real backup/restore exercise remain untested.

Reviewer follow-up confirmed the P1 fix and additional quarantine handling;
no new concrete defect was identified in that limited read-only re-review.
After those changes, the full actual-PostgreSQL `go test -race -count=1 ./...`
passed again. This is not a review of the unimplemented full integration.
