# OpenConnector compatibility gateway

Work in progress: multica-core-v1, local development only. The loopback-only development executable exposes catalog reads; connection
and session APIs are still unsupported. See the evidence for tested scope.

The adjacent authorized Multica checkout is required at `../../multica`.
The contract suite imports its real `server/pkg/composio` package.

```sh
go test -count=1 ./tests/contract
```

Use Go 1.26.6 and bounded task-local GOPATH, GOMODCACHE and GOCACHE. See
`docs/preflight.md`, `contracts/matrix.md`, `compatibility-manifest.yaml`
and `docs/acceptance.md` for evidence and remaining gates. Original user
materials are preserved byte-for-byte in `../t00-materials`.
