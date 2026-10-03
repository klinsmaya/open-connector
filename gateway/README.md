Current local candidate: [evidence and release gates](docs/t09-t12-evidence.md),
[operations and rollback](ops/RUNBOOK.md), [recovery handoff](docs/recovery-handoff.md).
Older evidence files describe earlier checkpoints; they are not the current status.

# OpenConnector compatibility gateway

Local candidate: multica-core-v1 with catalog, trusted-subject OAuth identity
verification, exact read-only MCP sessions, live Multica source authority,
durable execution/revocation and restore quarantine. The executable remains
loopback-only; real providers and production deployment are not approved.
See [configuration and deployment differences](ops/CONFIGURATION.md) and the
40-row acceptance evidence for the exact tested boundaries.

The adjacent authorized Multica checkout is required at `../../multica`.
The contract suite imports its real `server/pkg/composio` package.

```sh
go test -count=1 ./tests/contract
```

Use Go 1.26.6 and bounded task-local GOPATH, GOMODCACHE and GOCACHE. See
`docs/preflight.md`, `contracts/matrix.md`, `compatibility-manifest.yaml`
and `docs/acceptance.md` for evidence and remaining gates. Original user
materials are preserved byte-for-byte in `../t00-materials`.
