#!/usr/bin/env bash
set -euo pipefail

# a-Shell release 0.1, asset ID 208109158 (updated 2024-11-21). The artifact
# is XZ Utils 5.3.1alpha. Do not accept changed release bytes without review.
url=https://github.com/holzschu/a-Shell-commands/releases/download/0.1/xz.wasm
expected=b3c7dc3ec1fe4e4370796c2f6e341d1da0edb99582ab15ce3229ce3575ef81df
output="$(cd "$(dirname "$0")" && pwd)/xz.wasm"
temporary="$(mktemp)"
trap 'rm -f "$temporary"' EXIT
curl --fail --location --silent --show-error "$url" --output "$temporary"
actual="$(shasum -a 256 "$temporary" | cut -d ' ' -f 1)"
if [[ "$actual" != "$expected" ]]; then
  echo "xz artifact SHA-256 changed: $actual" >&2
  exit 1
fi
mv "$temporary" "$output"
