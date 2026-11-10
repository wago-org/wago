#!/usr/bin/env bash
set -euo pipefail

here=$(cd "$(dirname "$0")" && pwd)
tmp=$(mktemp -d "${TMPDIR:-/tmp}/wago-fasttree.XXXXXX")
trap 'rm -rf "$tmp"' EXIT

curl -fsSL https://biowasm.com/cdn/v3/fasttree/2.1.11/fasttree.wasm -o "$tmp/original.wasm"
curl -fsSL https://biowasm.com/cdn/v3/fasttree/2.1.11/fasttree.js -o "$tmp/fasttree.js"
printf '%s  %s\n' 18ceb801b13b694940577e41ecac3d8a6537cfdba54bc36719bbc38d1d0dc6d8 "$tmp/original.wasm" | shasum -a 256 -c -
node "$here/../../../tools/emscripten-v8-oracle.mjs" "$tmp/fasttree.js" "$tmp/original.wasm" "$here/inputs/alignment.fasta" -nt > "$tmp/stdout"
printf '%s  %s\n' ff79d3415c1bd8bbf930305f08289a324057d3505d209ee2ab1b85cb190cf98c "$tmp/stdout" | shasum -a 256 -c -
wasm2wat "$tmp/original.wasm" -o "$tmp/module.wat"
perl -pi -e '
 s/\(import "a" "a"/\(import "env" "__assert_fail"/;
 s/\(import "a" "b"/\(import "env" "exit"/;
 s/\(import "a" "c"/\(import "env" "gettimeofday"/;
 s/\(import "a" "d"/\(import "env" "__sys_fcntl64"/;
 s/\(import "a" "e"/\(import "wasi_snapshot_preview1" "fd_write"/;
 s/\(import "a" "f"/\(import "wasi_snapshot_preview1" "fd_close"/;
 s/\(import "a" "g"/\(import "wasi_snapshot_preview1" "fd_fdstat_get"/;
 s/\(import "a" "h"/\(import "env" "__sys_ioctl"/;
 s/\(import "a" "i"/\(import "wasi_snapshot_preview1" "fd_read"/;
 s/\(import "a" "j"/\(import "env" "__sys_open"/;
 s/\(import "a" "k"/\(import "env" "emscripten_fd_seek"/;
 s/\(import "a" "l"/\(import "env" "emscripten_memcpy_big"/;
 s/\(import "a" "m"/\(import "env" "emscripten_resize_heap"/;
 s/\(export "n" \(memory/\(export "memory" \(memory/;
 s/\(export "o" \(func/\(export "__wasm_call_ctors" \(func/;
 s/\(export "p" \(func/\(export "main" \(func/;
 s/\(export "s" \(func/\(export "stackAlloc" \(func/;
' "$tmp/module.wat"
wat2wasm "$tmp/module.wat" -o "$tmp/fasttree.wasm"
cmp "$tmp/fasttree.wasm" "$here/fasttree.wasm"
