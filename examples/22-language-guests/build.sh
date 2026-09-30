#!/usr/bin/env bash
set -euo pipefail

example_dir="$(cd "$(dirname "$0")" && pwd)"

wat2wasm "$example_dir/wat/answer.wat" -o "$example_dir/wat/answer.wasm"
npx --yes --package assemblyscript@0.28.8 asc \
  "$example_dir/assemblyscript/answer.ts" \
  --runtime stub \
  --optimize \
  --noAssert \
  --outFile "$example_dir/assemblyscript/answer.wasm"
tinygo build -target=wasm-unknown -no-debug \
  -o "$example_dir/tinygo/answer.wasm" \
  "$example_dir/tinygo/main.go"
