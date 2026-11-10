#!/usr/bin/env bash
set -euo pipefail

here=$(cd "$(dirname "$0")" && pwd)
tmp=$(mktemp -d "${TMPDIR:-/tmp}/wago-seqtk.XXXXXX")
trap 'rm -rf "$tmp"' EXIT

curl -fsSL https://biowasm.com/cdn/v3/seqtk/1.4/seqtk.wasm -o "$tmp/original.wasm"
curl -fsSL https://biowasm.com/cdn/v3/seqtk/1.4/seqtk.js -o "$tmp/seqtk.js"
printf '%s  %s\n' cd6d040b48c752d0bd669f3cc222883a2ad725839c0f8fefc36fb37c766407f9 "$tmp/original.wasm" | shasum -a 256 -c -
node "$here/../../../tools/emscripten-v8-oracle.mjs" "$tmp/seqtk.js" "$tmp/original.wasm" "$here/inputs/reads.fastq" seq -A - > "$tmp/stdout"
printf '%s  %s\n' 7e6e8898889ea9ea26296ab7b39e0d2c10c6de19de8c4b6c88dbb6947b6223ac "$tmp/stdout" | shasum -a 256 -c -
wasm2wat "$tmp/original.wasm" -o "$tmp/module.wat"
perl -pi -e 's/\(import "wasi_snapshot_preview1" "fd_seek"/\(import "env" "emscripten_fd_seek"/' "$tmp/module.wat"
wat2wasm "$tmp/module.wat" -o "$tmp/seqtk.wasm"
cmp "$tmp/seqtk.wasm" "$here/seqtk.wasm"
