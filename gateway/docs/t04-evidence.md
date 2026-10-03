# T04 authorization and native integration — 2026-10-03

Implemented a durable connect flow: a stable compatibility account ID and
nonce-authenticated browser page, one native request creation claim, exact
request polling, native account validation, and a separate one-time identity
ticket. Provider credentials remain in OpenConnector. Gateway storage keeps
only references and encrypted authorization URLs, plus token digests.

The project callback origin must be allowlisted and the callback path is
fixed to the Multica callback. URL success parameters never activate accounts.
Native create outcome uncertainty leaves CREATING and never repeats creation.
A known request resumes after process restart through its persisted ID and
ciphertext. Lost ticket redirects can be recovered by CAS ticket rotation;
old tickets immediately become unusable. Completion rechecks the native
request/account before consuming the ticket and committing activation.
Wrong login identity quarantines the candidate and records a blocked orphan.
Expired and superseded tickets fail closed. A completed ticket can retrieve
the same result for the same project/subject while it remains unexpired and
the mapping remains ACTIVE; it never repeats activation or revocation. This
allows recovery when the completion response or subsequent Multica mirror
write fails. Wrong-subject retries cannot alter an already completed binding.

Replacing an active mapping invalidates only sessions referencing that old
connection. Their runtime-token cleanup is queued. Old provider connection
cleanup is BLOCKED until a provider-specific decision establishes that remote
revocation cannot invalidate the replacement's shared grant. This is not yet
a working remote revoke/outbox processor.

Connected-account list/detail now expose compatibility IDs, user and nested
and top-level auth-config identity. Repeated array filters intersect before
pagination; signed cursors bind the query, project and result snapshot. Native
IDs and credentials are not in account responses. State is the gateway's
current durable state; periodic native expiry reconciliation remains future work.

## Actual tests and limits

`tests/integration/connect_test.go` launches the actual Node OpenConnector
HTTP app with SQLite, calls the gateway with the real Multica SDK, and stores
gateway state in PostgreSQL. It exercises both matching and mismatched
identity completion, actual native OAuth state/callback handling, forged
success query rejection, ticket response-loss recovery/rotation, account wire
shape and filtering, and credential exclusion from gateway transaction JSON.
The OAuth provider exchange is an explicitly offline fixture. It does not
contact a provider or use a real account, and this is not an agent/daemon E2E.

Store regressions cover duplicate creation claims, wrong nonce/project,
single-use tickets, mismatch quarantine, unrelated session preservation,
replacement session revocation, and direct hidden-action-config rejection.
Independent reviewer review_t02 found the over-broad session revocation,
lost-ticket recovery gap and catalog-admission mismatch; all were fixed and
regressed. Review also caught unsafe replacement-grant cleanup; it is BLOCKED.

The matching browser identity is supplied through the control boundary in the
gateway test. Multica's actual login middleware and verifier forwarding have
separate tests in its fork; do not describe the two tests as one complete
browser/daemon end-to-end run.

CLI flags now select one native runtime and its admin/vault secret files.
They do not provision a project or silently clear restore quarantine. T06
session issuance, MCP execution and T10 cleanup are not part of this slice.
