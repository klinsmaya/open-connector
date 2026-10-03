#!/usr/bin/env bash
set -euo pipefail
repo_root=$(cd "$(dirname "$0")/../.." && pwd)
output="$repo_root/.tmp/gateway-image"
mkdir -p "$output"
go_binary=${GATEWAY_GO:-go}
if [[ $("$go_binary" version) != 'go version go1.26.6 '* ]]; then
  echo 'Go 1.26.6 is required for this candidate.' >&2; exit 1
fi
CGO_ENABLED=0 "$go_binary" -C "$repo_root/gateway" build -trimpath -buildvcs=false -o "$output/gateway" ./cmd/gateway
cp /etc/ssl/certs/ca-certificates.crt "$output/ca-certificates.crt"
cp "$repo_root/gateway/ops/Dockerfile" "$output/Dockerfile"
chmod 0755 "$output/gateway"
chmod 0644 "$output/ca-certificates.crt"
sha256sum "$output/gateway" "$output/ca-certificates.crt" > "$output/SHA256SUMS"
export BUILDX_CONFIG="$output/buildx"
docker build --network=none --pull=false --iidfile "$output/image-id" -t open-connector-gateway:recovery-local "$output"
docker run --rm --network=none --read-only --cap-drop=ALL --security-opt=no-new-privileges open-connector-gateway:recovery-local -h
cat "$output/image-id"
