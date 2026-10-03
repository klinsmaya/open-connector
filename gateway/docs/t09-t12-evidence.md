# Local lifecycle, safety and recovery evidence

This work was implemented in the new environment. Neither old unpushed commit
was recovered. This is a development candidate; release remains blocked.

## T09–T10 behavior

Local disable commits connection generation invalidation, session revocation and
an outbox before remote work. Multica commits its owner-scoped local disable and
connection outbox atomically; live source authorization rejects old tasks before
the outbox runs. A 202 response explicitly reports local disable and pending
remote outcome. Settings display the backend and unresolved cleanup status.
Official-mode delete/revoke behavior is unchanged; compatibility mode does not
swallow unknown-account 404 responses.

Native OC has separate administrator-only POST `/v1/connections/by-id/{id}/revoke`
and DELETE `/v1/connections/by-id/{id}/revoked`. A successful revoke retains the
credential until explicit cleanup. UNKNOWN, UNSUPPORTED and REVOKING never
produce a successful revoke response or release their durable provider barrier.
Repeated calls read the persisted outcome; they never blindly redispatch a
provider revoke. Only providers explicitly listed in
`OOMOL_CONNECT_STRICT_REVOCATION_SERVICES` are eligible, with encrypted storage.
The default list is empty: no real provider grant behavior has been verified.
Operators must prove that the provider/client grant is exclusive to this OC
runtime; the database cannot discover grants held by another application.

The barrier serializes provider configuration, connection replacement, OAuth
work registration and trigger creation/update across processes. Shared local
connections, active/deleting triggers, pending requests/states and unfinished
OAuth network work block strict revoke. Returned but unpersisted OAuth tokens
are retained as encrypted recovery records. Unknown operations have no automatic
lease expiry. Rotating encryption while a revocation barrier exists fails
atomically; reconcile first. Unblocked orphan records participate in key rotation.
A mixed native-version deployment is unsupported.

Strictly configured providers reject the legacy DELETE path before any remote
I/O. Providers outside that opt-in keep the old default contract. Ordinary SQL
delete cannot cross a revocation barrier. Confirmed cleanup clears the barrier
and credential in one transaction. Completed gateway tombstones allow retry after
successful cleanup with a lost response. Replaced/shared-grant tasks stay BLOCKED.

## Evidence in this environment

- Native OC: 238 focused tests passed across SQLite, D1, PostgreSQL, connection
  service and OAuth suites. Includes actual PostgreSQL 17.11 instances, not only
  PGlite. Provider failure/429 behavior is simulated, not a live provider test.
- Gateway: PostgreSQL-backed race suite passed with real SDK, real OC process,
  encrypted SQLite, actual MCP SDK client, source authority, exact pins, operation
  replay/conflict, blocked cleanup and replacement. Forwarded-browser rejection
  now explicitly verifies retained encrypted native OAuth material.
- Multica: actual daemon claim HTTP handler against real gateway/native OC and
  two databases passed terminal, cancellation, permission withdrawal, connection
  withdrawal, explicit disconnect, agent deletion and membership withdrawal.
  Terminal sessions are still unexpired when rejected; all non-MCP claim fields
  are scanned for session/control/native/provider/source secrets. This is not a
  third-party Agent CLI execution.
- Read-only review found late legacy DELETE, lost cleanup response, and existing
  BLOCKED-job state inconsistencies; all were fixed with regression tests.
- Actual pg_dump/pg_restore of an isolated gateway schema passed. The restored
  database was quarantined before any listener opened; old bearers stayed denied
  after reopening, DISPATCHED became UNKNOWN, and ciphertext remained intact.
- Local scratch gateway image is built with Go 1.26.6, an explicit CA bundle,
  non-root UID, read-only filesystem, no capabilities and no network for smoke.
  This is a gateway-only build; native/Multica release images are not certified.

## Release gates still open

Target Agent CLIs have not been invoked. Multica CLAUDE.md requires explicit
real-agent authorization and a tagged smoke test because account access/quota
may be involved; the current authorization permits offline tests. Existing
Composio prompts/skills have not been certified against native-tools-v1 in those
CLIs. Real provider OAuth/revocation, deployment TLS/network enforcement, full
multiservice restore/canary, and production image signing remain unverified.
Do not treat the local fixture results as approval to release.

The inherited Multica migration uniqueness test fails on two baseline migration
468 names. Both exist in base a9e82c7. New migrations 469–475 use distinct numbers;
the real migration runner successfully applies their full names. No baseline
migration history was rewritten.

## Reproduction commands

Use independent disposable databases and Go 1.26.6; never point these tests at
production. Supply `GATEWAY_TEST_DATABASE_URL`, `OC_NATIVE_TEST_DATABASE_URL` and
Multica `DATABASE_URL` from local fixture configuration.

```
# open-connector
npm run fix-check
npx vitest run src/server/storage/sqlite/connection-safety.test.ts \
  src/server/storage/sqlite/runtime-store.test.ts \
  src/server/storage/postgres/connection-safety.test.ts \
  src/server/storage/postgres/runtime-store.test.ts \
  src/server/storage/d1/runtime-store.test.ts src/connection-service.test.ts \
  src/oauth/oauth-flow-service.test.ts src/oauth/oauth-credential-refresh-service.test.ts
go -C gateway test -race ./... -count=1
go -C gateway vet ./...
# Set GATEWAY_TEST_PG_CONTAINER=oc-recovery-pg only for the disposable dump/restore fixture.

# multica/server; GATEWAY_TEST_BINARY and GATEWAY_TEST_NATIVE_FIXTURE must be explicit.
../scripts/go-test-with-agent-cli-guard.sh go test -race ./internal/handler \
  -run 'Composio|Compatibility' -count=1
../scripts/go-test-with-agent-cli-guard.sh go test -race \
  ./pkg/composio ./internal/integrations/composio ./cmd/server
go vet ./pkg/composio ./internal/integrations/composio ./internal/handler ./cmd/server
# multica/packages/views, using the already installed package tools
./node_modules/.bin/vitest run settings/components/composio-tab.test.tsx
./node_modules/.bin/tsc --noEmit
./node_modules/.bin/eslint settings/components/composio-tab.tsx settings/components/composio-tab.test.tsx
```

The final local review found no additional immediate authorization/ciphertext
loss flaw in this increment. One operational limitation remains: the native
adapter conservatively maps native HTTP 409 (REVOKING, UNKNOWN or shared-grant
blocking) to gateway BLOCKED. It retains recovery data and requires operator
reconciliation; it does not claim remote success or repeat raw provider calls.
