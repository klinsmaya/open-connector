# Claim-time source authority and revocation

New work after checkpoint 671f1450 / ca12eec11; not recovered old commits.

Multica compatibility sessions are issued at daemon claim, never at enqueue.
Current invocation and source memberships are checked while registering the
run. Registry commit holds source authorization row locks, preventing withdrawal
triggers from missing an uncommitted registry entry. Trigger comparisons ignore
no-op updates, limit connection changes to the relevant toolkit and membership
changes to the registered workspace. Tombstones remain after source deletion.

Gateway issuance and every admission consult the configured Multica source
endpoint with a separate service token. Redirects, timeouts, source failures and
negative verdicts deny access; no source decision is cached. The CLI requires
this configuration whenever native gateway functionality is enabled. A nil store
hook remains an explicit fixture/embedding boundary, not a deployment default.

## Executed evidence

- PASS: `TestCompatibilityDaemonClaimLifecycleRealGateway`, six variants:
  completion, cancellation, toolkit permission change, connection revocation,
  Agent deletion and member removal. Actual gateway binary + native OC process,
  real SDK and both PostgreSQL schemas; provider exchange is an offline fixture.
- Each variant checks deferred issuance, fresh daemon claim, no project-key
  fallback, a scan of every non-MCP credential carrier in the claim (including
  prompts/instructions), immediate source denial before outbox delivery, durable
  revocation of an unexpired session and retained runtime-token ciphertext.
- PASS: actual two-connection registration/permission-withdrawal interleaving;
  PostgreSQL reports writer waiting on a lock, then outbox records withdrawal
  after registration commits. Both tests ran with Go's race detector.
- PASS: no-op updates and deletion of an unrelated workspace membership preserve
  the live task; official SDK/integration regressions; server package tests.
- PASS: gateway full PostgreSQL race suite. Actual MCP client freezes protocol
  2025-06-18 against official Go SDK v1.8.0. Other advertised SDK versions are not
  claimed tested merely because the dependency supports them.

These are daemon claim-handler integration tests, not a launched third-party
Agent CLI. Orphan OAuth credential recovery is distinct from the runtime-token
ciphertext assertion and remains incomplete. T09/T10 provider lifecycle and
T11/T12 full fault/restore/upgrade/rollback acceptance are not complete.

Local Multica fixture DB: `multica_recovery_test` on the existing disposable
loopback PostgreSQL container. Its baseline migrations and new 469–473 were
applied locally; no production database was contacted or migrated. Recovery
source documents remain unchanged.

## Existing baseline check failure

`go test ./internal/migrations` fails its numeric-prefix uniqueness lint because
`468_comment_deleted_at` and `468_drop_reference_only_column` both exist at
Multica's original base a9e82c79739446111b8ca9acbb256f584072d20d. `git ls-tree`
confirmed both at that exact base. New migrations use unique 469–473. The full
migration test package is therefore NOT PASS; historical migration identifiers
were not silently rewritten. The actual isolated migration runner accepted
these pre-existing distinct full filenames. Related package vet checks passed.
