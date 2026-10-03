# P0 evidence — 2026-10-03

Executed in the new recovery environment using Go 1.26.6:

```sh
GOPATH=/tmp/oc-recovery-tools/gopath \
GOCACHE=/tmp/oc-recovery-tools/build \
GOMODCACHE=/tmp/oc-recovery-tools/modules GOTOOLCHAIN=local \
/tmp/oc-recovery-tools/go/bin/go test -count=1 -v ./tests/contract
```

Result: PASS, four top-level tests and two status subtests. Real Multica SDK
is imported from the adjacent checkout. This proves client/fixture contracts
only; C01–C06 gateway acceptance remains NOT_RUN.

- Catalog/auth config wire parsing and distinct filter encoding: PASS.
- Repeated account filters and nested ownership fields: PASS.
- Delete 404 idempotence and 501 unsupported error propagation: PASS.
- Malformed successful JSON rejected: PASS.

OC `npm run fix-check`: PASS (lint fix, format fix, src/scripts/examples
typecheck). Original materials are excluded from formatting to preserve
verified bytes. All five document hashes and 21 source snapshot hashes
were checked after formatting.

No old test result is reused. No live provider, daemon E2E, security review,
release, deployment or restoration acceptance has been run.
