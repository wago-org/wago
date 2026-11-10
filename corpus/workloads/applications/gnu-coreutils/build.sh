#!/usr/bin/env bash
set -euo pipefail

here=$(cd "$(dirname "$0")" && pwd)
tmp=$(mktemp -d "${TMPDIR:-/tmp}/wago-gnu-coreutils.XXXXXX")
trap 'rm -rf "$tmp"' EXIT

for program in seq tr; do
  curl -fsSL "https://biowasm.com/cdn/v3/coreutils/8.32/$program.wasm" -o "$tmp/$program.original.wasm"
  curl -fsSL "https://biowasm.com/cdn/v3/coreutils/8.32/$program.js" -o "$tmp/$program.js"
done

printf '%s  %s\n' fb5a383f63a18a61062e67d9514d9cd60b56965c662030deff9435dcf7505f82 "$tmp/seq.original.wasm" | shasum -a 256 -c -
printf '%s  %s\n' 545193c0f7e251a1265e8656bd7c59bdbf6923583b5ba570b34db3b53bb855e2 "$tmp/tr.original.wasm" | shasum -a 256 -c -

node "$here/../../../tools/emscripten-v8-oracle.mjs" "$tmp/seq.js" "$tmp/seq.original.wasm" /dev/null 1 10 > "$tmp/seq.out"
printf '%s  %s\n' bf794518e35d7f1ce3a50b3058c4191bb9401e568fc645d77e10b0f404cf1f22 "$tmp/seq.out" | shasum -a 256 -c -
node "$here/../../../tools/emscripten-v8-oracle.mjs" "$tmp/tr.js" "$tmp/tr.original.wasm" "$here/inputs/records.txt" a-z A-Z > "$tmp/tr.out"
printf '%s  %s\n' 7fb2bedccbb849fe9c8bcc513aa0441be6c8db7aac9db38cd6230449d7904075 "$tmp/tr.out" | shasum -a 256 -c -

for program in seq tr; do
  wasm2wat "$tmp/$program.original.wasm" -o "$tmp/$program.wat"
  perl -pi -e 's/\(import "wasi_snapshot_preview1" "fd_seek"/\(import "env" "emscripten_fd_seek"/' "$tmp/$program.wat"
  wat2wasm "$tmp/$program.wat" -o "$tmp/$program.wasm"
  cmp "$tmp/$program.wasm" "$here/$program.wasm"
done
