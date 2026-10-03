# Current recovery handoff

The original environment/task was not changed or bypassed. Old local commits
OC 97f21dc2511b36d1d4b2a4d4a0a53445c4b99975 and Multica
07b02ea587abc5e2a3e2b8960c91398b089f19ad were not recovered. The original Library
ZIP could not be materialized through the supported path. The parent supplied
five complete source documents; `t00-materials/SHA256SUMS` verifies all five.
This checkout is a new implementation from those documents, not restored old code.

Only the authorized forks were edited. Both use `feat/composio-recovery`:

- OpenConnector base: 0535653ace7338adb346ccf564458eb7a57bdefd.
- Multica base: a9e82c79739446111b8ca9acbb256f584072d20d.
- Current tested OC code: 353533aa78a798491a0e455a54e32c362ef3406e.
- Current Multica: b2b01356b1ca588e29a6db43200c2ed0f847b2af.
- Gateway local image: sha256:7d127a7327f17758f8536ae8b540c4c229d4af8bd184939c86f5b6ac7e552199.

T00–T10 have local implementation and progressively stronger fixture evidence:
trusted browser identity, exact read-only sessions, claim-time live source
authority, task/permission tombstones, encrypted runtime separation, durable
operation IDs, connection revoke outboxes and non-destructive native barriers.
T11 includes independent review, 238 native-focused tests, gateway race tests,
real daemon claim lifecycle tests, prompt secret scanning, encrypted native
orphan retention and settings tests. T12 includes a local locked gateway image,
real isolated pg_dump/restore, migration checks and an operations/rollback guide.
These are not a full T00–T12 acceptance or a production readiness statement.

Actual Codex app-server/MCP protocol and a full local Multica/gateway/native
canary now pass without model/provider calls. The independent PostgreSQL
instance and service container have no network; restore quarantine, exact
orphan ciphertext retention, replay/UNKNOWN and compatibility-off rollback
are verified. The drill found and fixed native capability discovery being
misclassified as runtime auth after token issuance (OC 954b3c52).

Release remains blocked on model-mediated prompt/Skill behavior, real provider
OAuth/revocation and grant exclusivity, target TLS/network enforcement and
signed production images/authorized rollout. The inherited Multica duplicate migration 468 lint failure remains;
its production history was not rewritten. Native 409 subtypes currently map
conservatively to BLOCKED and require operator reconciliation.

See [current evidence](t09-t12-evidence.md), [all acceptance rows](acceptance.md)
and [operations](../ops/RUNBOOK.md). Old tests were not reused as new PASS.
No remote push, PR, main merge, production deployment, production migration or
permission expansion was performed. The original foundational Library artifact
must remain untouched; update only the established dual-repository checkpoint.
