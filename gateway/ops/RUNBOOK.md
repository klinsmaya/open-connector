# Candidate operations and release gate

This repository is a local development candidate. No production rollout is
approved. `docs/t09-t12-evidence.md` lists the unverified release gates.

## Build and freeze

Run `GATEWAY_GO=/path/to/go1.26.6 gateway/ops/build-local.sh` from the OC checkout.
It produces an unpushed local scratch image, `.tmp/gateway-image/image-id`, binary
and CA-bundle SHA256 values. Record both fork commits, tree hashes, Go/Node/MCP
versions and image ID together. Rebuild after any source change. There is no
floating base image. The CA bundle hash must be reviewed when it changes.
The sibling Multica checkout is required for the SDK contract tests in go.mod.

## Isolation and secrets

Use independent control (8080) and execution (8081) listeners. Put authenticated
TLS reverse proxies in front of browser/control and MCP endpoints. Only the
trusted Multica server can reach control APIs, native OC management, and the
source-authority endpoint. An Agent can reach only the execution listener.
Explicitly deny Agent access to native OC, the database and metadata services.
Gateway egress is limited to its database, configured native runtime and source
authority. Native OC alone reaches approved provider endpoints.

Mount the project key only into Multica, native admin key only into the gateway,
and provider OAuth secrets only into native OC. Mount gateway database URL,
native admin, vault key and source authority token as individual read-only files,
owned for the non-root runtime. Never put them in command arguments, images,
Agent environment, prompts or normal logs. Protect the Multica source token with
an independent random value of at least 32 characters. Native storage must use
an encryption key; keep its backup independent of database backups.

The gateway serves no default project/provider/action. Native and gateway
migrations are explicit. Initialize a fresh gateway with `-mode migrate`; it is
quarantined by default. Provision only reviewed project, callback origins,
subject, auth-config and exact read-action bindings. `auth_configured` and
`runtime_verified` are evidence gates, not catalog-presence flags. Keep real
provider/action allowlists empty until their integration tests pass. Open the
recovery barrier only after provisioning and review. Do not enable strict
provider revoke unless grant exclusivity and provider revocation scope are known.

## Limits and monitoring

Execution is read-only. The HTTP body limit is 1 MiB, request deadline 15 seconds,
native request deadline 10 seconds, source-authority deadline 5 seconds and MCP
session lifetime 1 hour. Each listener admits at most 64 concurrent requests;
PostgreSQL enforces 120 execution HTTP requests per session per minute across
replicas. These conservative limits are functional bounds, not load-test results.
Monitor outbox age, BLOCKED/UNKNOWN records, failed source authorization and
ciphertext recovery backlog. Logs/errors contain stable codes, not raw upstream
responses. Audit records correlate action operation IDs and outcomes without
arguments or returned provider data. Define retention only after reviewing
idempotency requirements; never purge UNKNOWN work to permit redispatch.

## Backup and restore

Stop ingress and all workers before restoring. Restore native OC, gateway and
Multica as one coordinated epoch; a single-database restore is insufficient.
Restore into isolated networks with no provider egress. Before starting any
gateway listener, run `-mode quarantine-restore -database-url-file <file>`.
This permanently invalidates restored sessions, quarantines pending OAuth work,
turns DISPATCHED into UNKNOWN and preserves ciphertext. Reconcile source task
state, connection ownership, runtime tokens, outbox leases and native OAuth
operations before reopening. Previously issued bearers must remain rejected.
Never automatically replay UNKNOWN operations. Rotate leaked keys only after
preserving decryptability and reconciling native revocation barriers.

The committed `TestActualBackupRestoreQuarantinesSessionsAndUnknownOperations`
executes real pg_dump/pg_restore only against the explicitly selected disposable
`oc-recovery-pg` / `oc_gateway_test` fixture. It does not certify a full production
multiservice backup or native credential disaster recovery.

## Upgrade, canary and rollback

Freeze both fork commits and migrate a disposable copy first. Run all contract,
source-authority, lifecycle, MCP, native and restoration tests with explicit test
database URLs. Check migration digests: changing an already-recorded gateway
migration stops startup. Native security-capability revision 2 and subject
namespacing are required by gateway startup; an older native binary closes compat.
Do not run mixed native versions around a revocation barrier.

A future canary must use a separate project, callback origin, database and fresh
source tasks; use read-only fixture accounts before approving real providers.
There is no production canary evidence in this handoff.

For rollback, first stop new compatibility claims and ingress, drain or disable
workers, revoke sessions and retain the durable tombstones and recovery data.
Set Multica `COMPOSIO_BACKEND=official` with a separately configured official
project key and official endpoint. Never reuse a compatibility key/connection ID
as an official credential. Do not migrate OAuth credentials automatically.
Reconnecting official accounts is a separate user action. Do not reverse native
0018 or Multica lifecycle migrations while recovery records remain. An older
binary without the mandatory patches must run with compatibility disabled;
there is no fallback to a project key or an unrestricted runtime token.
