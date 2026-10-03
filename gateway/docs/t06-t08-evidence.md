# Session and read-only MCP slice

This is a new implementation in the recovery environment, not recovered old
source. T00–T12 and daemon E2E are not complete.

- Exact native token grants derive from current owner connections and admitted
  actions. Empty/wildcard grants fail closed. Native bearer ciphertext is
  isolated from the per-session gateway bearer.
- Policy revisions cannot regress or change content at the same revision.
  Changed policy revokes existing sessions. Task revocation has a durable
  tombstone, serialized against session preparation.
- Admission rechecks project, subject, policy/connection generation, capability
  admission, expiry and task revocation on every request/tool call.
- Official MCP Go SDK v1.8.0 provides stateless Streamable HTTP. Five explicit
  native-tools-v1 tools expose granted connections and read actions only.
- Durable operations bind project/subject/agent/actor/task, connection generation,
  action and canonical input. Conflicts reject. DISPATCHED/UNKNOWN never
  redispatch. Successful results replay without another native call.
- Runtime-token cleanup uses leased outbox jobs. Unknown mint remains pending,
  failed cleanup retains ciphertext, and late mint completion atomically
  schedules cleanup even after the original lease revoked the session.
- Provider grant revocation remains BLOCKED pending non-destructive native work.

## New-environment evidence

PASS: gateway `go test -race ./... -count=1` against disposable PostgreSQL 17.11;
then affected store/integration race suites after late-mint fix. Real Composio
SDK → gateway → PostgreSQL → real OC HTTP/SQLite → actual MCP SDK client covers
identity matching/mismatch, exact runtime mint, discovery/guide/read, operation
replay/conflict, denied selector/action, unexpired session revocation, native
token deletion and retained ciphertext. Provider exchange and read output are
offline fixtures; no real provider or daemon result is claimed.

PASS: OC `npm run fix-check`; gateway `go vet ./...`; Multica SDK/integration
 tests, including private compatibility requests rejecting official mode before I/O.

Read-only review identified and fixed: missing terminal task tombstone, missing
continuous capability checks, and late mint losing cleanup scheduling.

## Remaining boundaries

Multica claim-time issuance and lifecycle propagation are not yet wired. These
store/integration tests do not replace requested daemon terminal, prompt secret
scan and orphan retention assertions. HTTP action compatibility, non-destructive
provider revoke, live source-authority checks, migration/recovery and rollback
drills remain outstanding. No production readiness claim.
