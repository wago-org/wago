#!/usr/bin/env bash
set -euo pipefail

here=$(cd "$(dirname "$0")" && pwd)
tmp=$(mktemp -d "${TMPDIR:-/tmp}/wago-seq-align.XXXXXX")
trap 'rm -rf "$tmp"' EXIT
base=https://biowasm.com/cdn/v3/seq-align/2017.10.18

for spec in \
  lcs:54d4376352d4c469728f2e75852b06fc5079bfc8cabc0259494d389bbe7848b6 \
  needleman_wunsch:44ce58a899b679fb75a00c1252cef49df458448d8fd77a6083bd00be102be17b \
  smith_waterman:3376540d3596497ab50cf1ff4d008cf2e8d020fe433afab052fee27fd2557083
do
  name=${spec%%:*}
  digest=${spec#*:}
  curl -fsSL "$base/$name.wasm" -o "$tmp/$name.original.wasm"
  curl -fsSL "$base/$name.js" -o "$tmp/$name.js"
  printf '%s  %s\n' "$digest" "$tmp/$name.original.wasm" | shasum -a 256 -c -
  wasm2wat "$tmp/$name.original.wasm" -o "$tmp/$name.wat"
done

node "$here/../../../tools/emscripten-v8-oracle.mjs" "$tmp/lcs.js" "$tmp/lcs.original.wasm" /dev/null ACGTTGCA > "$tmp/lcs.out"
printf '%s  %s\n' de84eeff25bc787e35ddf73c9de4f48301ef0071c2cbf38922be70c22d8682d8 "$tmp/lcs.out" | shasum -a 256 -c -
node "$here/../../../tools/emscripten-v8-oracle.mjs" "$tmp/needleman_wunsch.js" "$tmp/needleman_wunsch.original.wasm" /dev/null ACGTTGCA ACGTCGCA > "$tmp/needleman_wunsch.out"
printf '%s  %s\n' 1e3fdaf624fe5dbc63c13ea6df1f1df5ceb13258925b5ae7b5e876d9e78f7f4d "$tmp/needleman_wunsch.out" | shasum -a 256 -c -
node "$here/../../../tools/emscripten-v8-oracle.mjs" "$tmp/smith_waterman.js" "$tmp/smith_waterman.original.wasm" /dev/null ACGTTGCA ACGTCGCA > "$tmp/smith_waterman.out"
printf '%s  %s\n' 1f3b6d8f053160f6232ce784988485d84c5c4cacafd87ae8680e713b3bcb75f6 "$tmp/smith_waterman.out" | shasum -a 256 -c -

perl -pi -e '
 s/\(import "a" "a"/\(import "env" "exit"/;
 s/\(import "a" "b"/\(import "wasi_snapshot_preview1" "fd_write"/;
 s/\(import "a" "c"/\(import "env" "emscripten_fd_seek"/;
 s/\(import "a" "d"/\(import "env" "emscripten_memcpy_big"/;
 s/\(import "a" "e"/\(import "env" "emscripten_resize_heap"/;
 s/\(import "a" "f"/\(import "wasi_snapshot_preview1" "fd_close"/;
 s/\(export "g" \(memory/\(export "memory" \(memory/;
 s/\(export "h" \(func/\(export "__wasm_call_ctors" \(func/;
 s/\(export "j" \(func/\(export "main" \(func/;
 s/\(export "k" \(func/\(export "stackAlloc" \(func/;
' "$tmp/lcs.wat"

for name in needleman_wunsch smith_waterman; do
  perl -pi -e '
   s/\(import "a" "a"/\(import "env" "exit"/;
   s/\(import "a" "b"/\(import "wasi_snapshot_preview1" "fd_write"/;
   s/\(import "a" "c"/\(import "env" "__sys_fcntl64"/;
   s/\(import "a" "d"/\(import "wasi_snapshot_preview1" "fd_close"/;
   s/\(import "a" "e"/\(import "wasi_snapshot_preview1" "fd_read"/;
   s/\(import "a" "f"/\(import "env" "emscripten_memcpy_big"/;
   s/\(import "a" "g"/\(import "env" "emscripten_resize_heap"/;
   s/\(import "a" "h"/\(import "env" "__sys_ioctl"/;
   s/\(import "a" "i"/\(import "env" "__sys_open"/;
   s/\(import "a" "j"/\(import "env" "emscripten_fd_seek"/;
   s/\(import "a" "k"/\(import "env" "__assert_fail"/;
   s/\(export "l" \(memory/\(export "memory" \(memory/;
   s/\(export "m" \(func/\(export "__wasm_call_ctors" \(func/;
   s/\(export "o" \(func/\(export "main" \(func/;
   s/\(export "q" \(func/\(export "stackAlloc" \(func/;
  ' "$tmp/$name.wat"
done

for name in lcs needleman_wunsch smith_waterman; do
  wat2wasm "$tmp/$name.wat" -o "$tmp/$name.wasm"
  cmp "$tmp/$name.wasm" "$here/$name.wasm"
done
