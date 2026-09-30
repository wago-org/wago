#!/usr/bin/env bash
set -euo pipefail

here=$(cd "$(dirname "$0")" && pwd)
tmp=$(mktemp -d "${TMPDIR:-/tmp}/wago-sed.XXXXXX")
trap 'rm -rf "$tmp"' EXIT

curl -fsSL https://biowasm.com/cdn/v3/sed/4.8/sed.wasm -o "$tmp/original.wasm"
curl -fsSL https://biowasm.com/cdn/v3/sed/4.8/sed.js -o "$tmp/sed.js"
printf '%s  %s\n' 0b4657e74593059737a40d37c3fa6a77e163618f44aae923da72a05536cba3fd "$tmp/original.wasm" | shasum -a 256 -c -
node "$here/../../../tools/emscripten-v8-oracle.mjs" "$tmp/sed.js" "$tmp/original.wasm" "$here/inputs/records.txt" -e 's/error/ERROR/g; s/warning/WARN/g' > "$tmp/stdout"
printf '%s  %s\n' a2c763a7c5947ca809df136a89a212ca0de22198c8285501fe711cea3156ef2e "$tmp/stdout" | shasum -a 256 -c -
wasm2wat "$tmp/original.wasm" -o "$tmp/module.wat"
perl -pi -e 's/\(import "wasi_snapshot_preview1" "fd_seek"/\(import "env" "emscripten_fd_seek"/' "$tmp/module.wat"
wat2wasm "$tmp/module.wat" -o "$tmp/sed.wasm"
cmp "$tmp/sed.wasm" "$here/sed.wasm"
