#!/usr/bin/env bash
set -euo pipefail

# kkebo/swift-format release 603.0.0-wasm32-wasi, commit
# 92097d54ac3be47738fe77e38c918e9aabce0302.
url=https://github.com/kkebo/swift-format/releases/download/603.0.0-wasm32-wasi/swift-format.wasm
expected=4f9c01a9569407766947c6fa6e9369dd9703f461f31dc9c79b49a6a6bcc5bc76
output="$(cd "$(dirname "$0")" && pwd)/swift-format.wasm"
temporary="$(mktemp)"
trap 'rm -f "$temporary"' EXIT
curl --fail --location --silent --show-error "$url" --output "$temporary"
actual="$(shasum -a 256 "$temporary" | cut -d ' ' -f 1)"
if [[ "$actual" != "$expected" ]]; then
  echo "swift-format artifact SHA-256 changed: $actual" >&2
  exit 1
fi
mv "$temporary" "$output"
