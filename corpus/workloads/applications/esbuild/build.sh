#!/usr/bin/env bash
set -euo pipefail

# Rebuild with Go 1.26.5 and compare the SHA-256 in corpus/catalog.json before
# updating the committed artifact. The upstream esbuild revision is immutable.
revision=f6058f8364fe7ab91ca57a83e02577ed74c9cae4
source_dir="${1:?pass a clean esbuild checkout at the pinned revision}"
output="$(cd "$(dirname "$0")" && pwd)/esbuild.wasm"
if [[ "$(git -C "$source_dir" rev-parse HEAD)" != "$revision" ]]; then
  echo "esbuild checkout must be at $revision" >&2
  exit 1
fi
if [[ -n "$(git -C "$source_dir" status --porcelain)" ]]; then
  echo 'esbuild checkout must be clean' >&2
  exit 1
fi
if [[ "$(go version)" != 'go version go1.26.5 '* ]]; then
  echo 'build with Go 1.26.5' >&2
  exit 1
fi
(
  cd "$source_dir"
  CGO_ENABLED=0 GOOS=wasip1 GOARCH=wasm GOFLAGS=-mod=readonly go build -o "$output" ./cmd/esbuild
)
shasum -a 256 "$output"
