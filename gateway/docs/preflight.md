# Recovery preflight — 2026-10-03

## Authorized workspace

Only `klinsmaya/open-connector` and `klinsmaya/multica` may be changed.
The gateway lives in `/workspace/open-connector/gateway`. Both origin URLs
were verified as the corresponding klinsmaya repositories. Both initial
working trees were clean on branch `work`. Local branch
`feat/composio-recovery` was created in both repositories without resetting
or deleting any files. Remote push, PR creation, main merges, production
writes/migrations/deployments, permission expansion and paid resources
remain prohibited. The previous task/environment has not been modified.

| Repository     | Initial HEAD                             |
| -------------- | ---------------------------------------- |
| open-connector | 0535653ace7338adb346ccf564458eb7a57bdefd |
| multica        | a9e82c79739446111b8ca9acbb256f584072d20d |

The Multica research baseline `2ea01ae4ef55de4310b99af192d2dbd367832883`
was fetched read-only from the authorized fork. Its `server/pkg/composio`,
`server/internal/integrations/composio` and `server/internal/runtimeapps`
trees have no diff against the selected base. `server/cmd/server/router.go`
has 27 insertions and 189 deletions; integration wiring must be located in
the current tree. No deployment version was supplied or inferred from HEAD.

## Recovery evidence

The old OC commit `97f21dc2511b36d1d4b2a4d4a0a53445c4b99975` and Multica
commit `07b02ea587abc5e2a3e2b8960c91398b089f19ad` are absent from local
object stores. GitHub read requests for both returned 422, no commit found.
`git fsck --no-reflogs --unreachable` reported no recoverable objects.
The old `/workspace/deliverables/multica-core-v1` directory and gateway did
not exist. Scoped source searches found no relevant bundle or patch.
Library search found design documents, not recovered implementation code.
This is a reconstruction from requirements, not recovery of the old code.

Library identified ZIP `libfile_ee5e52d578708191b57fbe9f75971f2a`, size
38,995 bytes, but its supported materialization failed. No bypass was used.
The parent subsequently transferred five full UTF-8 documents as text and
sent ATTACHMENT TEXT TRANSFER COMPLETE. All five reconstructed byte counts
and SHA-256 hashes match. See `../../t00-materials/SHA256SUMS`.
This verifies the transferred documents, not the unreadable ZIP container.
No attachment script was executed.

## Instructions and tools

Read OC AGENTS.md and CONTRIBUTING.md; Multica AGENTS.md, CLAUDE.md,
CONTRIBUTING.md and naming conventions; both CI workflows and package
scripts. OC requires native TypeScript, oxlint/oxfmt and `npm run fix-check`.
Multica requires Go formatting/vet, relevant tests, no new foreign keys,
and concurrent indexes in separate migration files. Default tests must
not execute ambient authenticated agent CLIs; use the repository guard.

Observed tools: Node 24.19.0, npm 11.9.0, pnpm 11.19.0, Docker 28.4.0.
Node matches OC CI major version. Multica pins pnpm 10.28.2; frontend tests
must use that version when needed. Existing OC vitest 4.1.11 and oxfmt
0.56.0 execute. Docker daemon responds but its image list is initially empty.
No psql/initdb/pg_ctl was on PATH. `/usr/bin/go` is not the Go compiler.
Go 1.26.6 was downloaded from go.dev and its official SHA-256 verified:
`708effb774be8237570d0add163225abbdfaf4fca28b2611df167beba4feef89`.
Compiler path: `/tmp/oc-recovery-tools/go/bin/go`. No system tool was changed.

At initial inspection `/workspace` had approximately 30 GiB available
(32 GiB filesystem, 1% used); `/tmp` had 8.8 GiB available. Keep all new
build/module caches explicitly scoped under `/tmp/oc-recovery-tools`, run
narrow checks first, and check disk before broader integration builds.
Do not remove another task's caches or files.

## Risks and remaining gates

- Old implementation and uncommitted daemon E2E assertions were not recovered.
- Rebuild tests for terminal-task revocation of unexpired sessions, prompt
  secret scanning and retained orphan ciphertext; old PASS evidence is invalid.
- No production or live-provider credentials/resources were authorized.
  L3 and production release/canary remain BLOCKED, not mock-verified.
- PostgreSQL integration is NOT_RUN until an isolated local instance exists.
- Runtime session Bearer handling, trusted browser identity, OC subject
  namespace and non-destructive revoke do not exist at these baselines.
- Multica task overlay creation currently logs a failure and continues;
  `server/internal/handler/daemon.go` merges the queued overlay at claim and
  falls back to agent config on merge errors. Compat must fail closed here.
- All 40 acceptance entries start NOT_RUN. No integration completion claimed.

## Verification and rollback

Executed read-only git remote/status/HEAD/worktree/reflog/object checks,
GitHub commit reads, document hash checks, tool version checks and source
diffs. These are preflight evidence only. No application tests have passed
in this environment yet. No production migration or service was started.
New branches preserve both base commits; abandon only this task's changes
or revert its local commits to roll back. Do not reset/clean the workspace.
