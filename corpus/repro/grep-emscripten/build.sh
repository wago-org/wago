#!/usr/bin/env bash
set -euo pipefail

here=$(cd "$(dirname "$0")" && pwd)
tmp=$(mktemp -d "${TMPDIR:-/tmp}/wago-grep.XXXXXX")
trap 'rm -rf "$tmp"' EXIT

curl -fsSL https://biowasm.com/cdn/v3/grep/3.7/grep.wasm -o "$tmp/original.wasm"
curl -fsSL https://biowasm.com/cdn/v3/grep/3.7/grep.js -o "$tmp/grep.js"
printf '%s  %s\n' 601e3f5a11098349169928298f4b37f8f29fe35d38e7c1dcc7377a99aa6f3c86 "$tmp/original.wasm" | shasum -a 256 -c -
node "$here/../../tools/emscripten-v8-oracle.mjs" "$tmp/grep.js" "$tmp/original.wasm" "$here/inputs/records.txt" error - > "$tmp/stdout"
printf '%s  %s\n' 7c7131c284ea18173f0d391ccb0a8a59989cb896d125d13f7b68b5b05d92b74c "$tmp/stdout" | shasum -a 256 -c -
wasm2wat "$tmp/original.wasm" -o "$tmp/module.wat"
perl -pi -e 's/\(import "wasi_snapshot_preview1" "fd_seek"/\(import "env" "emscripten_fd_seek"/' "$tmp/module.wat"
wat2wasm "$tmp/module.wat" -o "$tmp/grep.wasm"
cmp "$tmp/grep.wasm" "$here/grep.wasm"
