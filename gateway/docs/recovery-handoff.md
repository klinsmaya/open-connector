# Recovery checkpoint — 2026-10-03

This is a partial reimplementation, not recovery of the previous implementation.
No old PASS result is used as evidence. No remote pushes, PRs, main merges,
production access or migrations were performed. Changes are confined to the
authorized OpenConnector fork; the Multica fork has only a local development
branch and remains at its original commit with a clean worktree.

## Source recovery

- OpenConnector base: `0535653ace7338adb346ccf564458eb7a57bdefd`.
- Multica base: `a9e82c79739446111b8ca9acbb256f584072d20d`.
- Old local commits `97f21dc2511b36d1d4b2a4d4a0a53445c4b99975` and
  `07b02ea587abc5e2a3e2b8960c91398b089f19ad` were not found through supported
  sources. No old bundle or patch was recovered.
- Official Library materialization of the referenced ZIP failed. The parent
  supplied the five complete documents instead. They are saved byte-for-byte
  under `t00-materials` and validated by `SHA256SUMS`; see preflight.md.
- The old task and environment were not modified or bypassed.

## Local implementation

Branch in both forks: `feat/composio-recovery`. OpenConnector commits:

1. `65ec8190`: source/SDK contract freeze and real Multica SDK fixture tests.
2. `08748f6a`: independent Go gateway, PostgreSQL migrations, fail-closed
   admission, credential separation, session revoke/outbox and explicit restore
   quarantine. New databases are quarantined by default.
3. `d104bd23`: project-scoped toolkit/auth-config APIs and signed cursors,
   exercised with the real Multica SDK and disposable PostgreSQL.
4. `c0768d5e`: opt-in native trusted-subject OAuth transaction namespaces.

The gateway does not yet offer a usable read-only end-to-end integration.
Unsupported routes return explicit errors. Configuration capability records
in tests are synthetic; there is no verified native provisioning mechanism.
The command has no bootstrap/unquarantine UI or management flow. Restore
quarantine is an explicit operation, not automatic restored-snapshot detection.

## Verification in this container

- Gateway `go test -race -count=1 ./...`: PASS with an isolated PostgreSQL URL.
- Gateway `go vet ./...`: PASS for the T02 implementation.
- Native MCP/runtime baseline: 57 tests PASS.
- Native connection/auth/runtime tests after T05: 54 tests PASS.
- `npm run fix-check`: PASS after T05 source changes.
- Five transferred document SHA-256 checks: PASS.
- Independent read-only review found two fixed issues: NULL array entries
  weakening SQL policy checks, and live usage reordering signed cursor pages.
  T05 native scope review found no specific defect; hosted SaaS browser
  completion remains excluded. See evidence files for detailed scope.

Do not count these as daemon E2E, live OAuth or live remote revocation results.
The three previously uncommitted daemon assertions were not recovered or run:
terminal task revokes an unexpired session; task prompt secret scan; orphan
ciphertext retention. Store tests do not substitute for those assertions.

## Remaining implementation, in order

1. Complete T01 MCP SDK/protocol freeze and actual transport tests.
2. T04 native authorization state machine: exact request/result correlation,
   CAS, one-time verifier ticket, actual Multica browser identity confirmation,
   restart recovery and orphan cleanup. Never activate from query success.
3. T06 session issuance using exact native connection/action grants; persist
   and encrypt upstream runtime credentials, include task/actor context and
   scope all admission to current project/subject/agent policy.
4. T07 Multica compat-only wiring, response headers, endpoint allowlist,
   no project-key fallback, claim-time issuance and lifecycle revocation outbox.
   Official mode must stay covered independently.
5. T08/T09 MCP metadata and native HTTP action dispatch with durable operation
   identity, conflict checks and UNKNOWN outcomes that are never blindly retried.
6. T10 non-destructive native revoke, retaining ciphertext on failed/unsupported
   or unknown outcome; durable concurrent replacement/trigger-resource checks
   are required before remote revocation. Existing native disconnect deletes
   first and must not be used as strict revoke.
7. T11/T12 full offline chain, daemon assertions, security review and isolated
   upgrade/rollback/restore drills. Live provider credentials/authorized test
   resources are not supplied; production operations remain forbidden.

## Local tools and disk

Go 1.26.6 is installed under `/tmp/oc-recovery-tools/go`, verified against the
published checksum. `/usr/bin/go` is not the compiler. Set GOPATH, GOCACHE and
GOMODCACHE under `/tmp/oc-recovery-tools` and GOTOOLCHAIN=local.

Disposable PostgreSQL container: `oc-recovery-pg`, loopback port 55432,
PostgreSQL 17.11. The fixture database is `oc_gateway_test`; credentials are
synthetic (`oc_recovery` / `oc_recovery_fixture`). Tests allocate and remove
random schemas. It is left available for continued local work. No unrelated
container was changed. At this checkpoint `/workspace` has 28 GiB free and
`/tmp` has 8.2 GiB free. Avoid unnecessary cache expansion.
