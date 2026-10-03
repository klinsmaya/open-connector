# Actual CLI protocol and isolated multiservice canary

Date: 2026-10-03. These are new executions in the recovered development
workspace. No old PASS result, model account, real provider credential or
production service was used. All source changes remain on local fork branches.

## Actual available CLI

The platform-installed, read-only `/opt/codex/bin/codex` reports
`codex-cli 0.159.0-alpha.3`; no CLI was installed or upgraded. SHA256:
`981ade7b03926534c654fd718ced3a9f378b7b2841271e29156f939462d176e9`.
Claude and OpenCode were not installed and were not tested.

Multica `TestCompatibilityOfflineCodexActualCLI` starts the real daemon claim
handler, gateway and native OC fixture. It passes only the newly issued session
MCP descriptor to a separate Docker container. The actual Multica Codex config
renderer configures the actual Codex app-server. Initialization, MCP discovery,
list_connections and execute_action pass. No `turn/start` or model call occurs;
the model endpoint is deliberately unreachable loopback. This certifies protocol
integration, not model reasoning, prompt adherence or third-party success.

Both test files require `agentintegration`; the CLI test checks
`MULTICA_RUN_REAL_AGENT_SMOKE=1` before using explicitly supplied binary paths.
Default tests do not discover or execute installed agent CLIs. The container is
non-root, read-only, capability-free, `network=none`, with a private tmpfs home
supplied by synthetic passwd. No host home, auth/config directory, Docker socket,
proxy or model credential is mounted/inherited. Its Unix HTTP proxy permits only
the exact session path. Docker uses an immutable local image ID and
`--pull=never`; network-none alone would not prevent a daemon image pull.

## Isolated local deployment

`TestCompatibilityIsolatedMultiServiceCanary` makes an actual PostgreSQL custom
archive and SQLite online backup from its quiescent claim fixture. The archive
restores into a new PostgreSQL 17.11 container with `network=none`, tmpfs data and
an independent fixture password. The original test database is asserted absent
in that instance. A second non-root, read-only, network-none container starts
actual Multica server, gateway and native OC processes. Its only external socket
reaches the new PostgreSQL instance; source, binaries and public libraries are
read-only mounts, and each child receives an explicit synthetic environment.
No provider egress, real OAuth exchange, model turn or production migration occurs.

Verified assertions:

- Restore quarantine runs before gateway ingress; startup fails while quarantined.
  Restored sessions stay invalid after explicit fixture reconciliation/reopening.
  Encrypted runtime-token bytes are retained.
- A separate native OAuth connection with no gateway mapping is created. Its exact
  native ID and ciphertext SHA256 survive an actual SQLite backup and restart
  after removing the previous WAL/SHM, and remain identical after rollback.
- Fresh scoped session reads traverse actual Multica authority HTTP, gateway and
  native OC. Two identical operation IDs execute once. A native action with a
  deliberately lost response becomes UNKNOWN; repeating it never redispatches.
- Rollback restarts Multica with compatibility disabled, no official project key
  and its feature flag off. The existing bearer is denied immediately and after
  gateway restart. UNKNOWN records and native credentials remain retained.
  This is a safe disable rollback; no downgrade migration or official reconnect
  was attempted.

The drill exposed a real native restart bug: capability discovery was assigned
runtime auth and rejected administrator probes once runtime tokens existed.
The regression failed before the fix; the exact capability route now uses admin
scope and still requires administrator bearer at the handler. Runtime tokens,
cookies and anonymous requests gain no new permissions.

The local service processes share a test container. This verifies external
isolation and protocol behavior, not production inter-service network policy or
process-compromise secret boundaries. The separately isolated Codex test covers
the Agent-to-session boundary. Production TLS, image signing, resource sizing
and rollout topology still require the actual target environment and approval.

## Latest executions and review

- Actual Codex offline protocol: PASS (1.24 seconds outer test).
- Isolated multiservice canary: PASS (4.92 seconds).
- Native auth and connection route regressions: 40 tests PASS; new regression
  previously failed with admin receiving 401.
- Gateway integration race suite: PASS (2.638 seconds), including exact retained
  native ID after forwarded-browser rejection.
- Default guarded Multica daemon lifecycle race suite: PASS (6.368 seconds),
  covering terminal/cancel/permission/connection/disconnect/agent/member cases.
- OpenConnector `npm run fix-check`: PASS.
- Independent read-only review: closed the database reachability and non-specific
  orphan assertions; no remaining concrete blocker or permission expansion found.

Binary SHA256 used for this drill:

| Binary         | SHA256                                                           |
| -------------- | ---------------------------------------------------------------- |
| Node 24.19.0   | bc17c508ffeed0ec622934f9b7fa72f8e78da65350e63c3eceb56fa688aa5e12 |
| Multica server | e4c2edcb8a059c66174b5f2f3d2543b28746a57d193e770b34adc392b5cdbd92 |
| Gateway        | dac51fdf9e28081a120b278200e091e974cdcebef4a1bf1d7c121af229d77f83 |

## Reproduce only in the disposable environment

Use Go 1.26.6 and existing dependency caches. Build `./cmd/server` with
`CGO_ENABLED=0` in Multica/server and the gateway command in its own module.
Build the static CLI probe with `go test -c -tags=agentintegration ./pkg/agent`.
Set explicit paths `GATEWAY_TEST_BINARY`, `GATEWAY_TEST_NATIVE_FIXTURE`,
`MULTICA_CODEX_BINARY`, `MULTICA_CODEX_PROBE_BINARY` and `MULTICA_CLI_IMAGE`
(the already-built immutable scratch image ID). The outer test uses only the
explicit disposable `DATABASE_URL` on `127.0.0.1:55432/multica_recovery_test`.

```
# Multica/server: actual CLI; no default-test guard because this is explicitly tagged.
MULTICA_RUN_REAL_AGENT_SMOKE=1 go test -tags=agentintegration ./internal/handler \
  -run '^TestCompatibilityOfflineCodexActualCLI$' -count=1 -v

# Additional explicit canary environment:
# GATEWAY_TEST_PG_CONTAINER=oc-recovery-pg
# GATEWAY_TEST_OC_ROOT=/workspace/open-connector
# GATEWAY_TEST_NODE_BINARY=<verified local Node 24 binary>
# GATEWAY_TEST_MULTICA_BINARY=<locally compiled static Multica server>
MULTICA_RUN_LOCAL_CANARY=1 go test -tags=agentintegration ./internal/handler \
  -run '^TestCompatibilityIsolatedMultiServiceCanary$' -count=1 -v
```

## Remaining external conditions

- Model-mediated prompt/Skill acceptance: approved model account and explicit
  permission to invoke it; direct offline RPC cannot establish this result.
- Real OAuth and remote revoke: approved test provider/client/account, exact read
  scopes, known grant exclusivity/revocation semantics and permitted provider
  network access. Provider allowlists remain empty until those tests pass.
- Target release: deployment topology, TLS endpoints, image/signing destination
  and explicit deployment/migration authority. None is authorized in this task.
- Duplicate baseline migration 468: actual deployed migration ledger and owner
  decision are needed before considering historical repair. It remains unchanged.
