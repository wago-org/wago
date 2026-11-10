#!/usr/bin/env sh
set -eu
here=$(cd "$(dirname "$0")" && pwd)
root=$(cd "$here/../../../.." && pwd)
repo=https://github.com/JuliaStrings/utf8proc.git
rev=0075ed7d0adba45682ee6bf7a83b10f8fd110163
upstream="$root/.tmp/upstream/utf8proc"
sdk=${WASI_SDK:-/opt/wasi-sdk}
if [ ! -x "$sdk/bin/clang" ]; then printf 'utf8proc: set WASI_SDK\n' >&2; exit 1; fi
if [ ! -d "$upstream/.git" ]; then git clone --filter=blob:none --no-checkout "$repo" "$upstream"; fi
git -C "$upstream" fetch --depth=1 origin "$rev" >/dev/null 2>&1
git -C "$upstream" checkout --detach "$rev" >/dev/null 2>&1
stage=$(mktemp -d); trap 'rm -rf "$stage"' EXIT
"$sdk/bin/clang" --target=wasm32-wasip1 -O2 -DNDEBUG -nostartfiles -ffunction-sections -fdata-sections -I"$upstream" \
	"$upstream/utf8proc.c" "$here/wago_utf8proc.c" \
	-Wl,--strip-debug -Wl,--no-entry -Wl,--export=utf8proc_run -Wl,--export-memory \
	-o "$stage/utf8proc.wasm"
got=$(shasum -a 256 "$stage/utf8proc.wasm" | awk '{print $1}')
want=$(shasum -a 256 "$here/utf8proc.wasm" 2>/dev/null | awk '{print $1}')
if [ "$got" != "$want" ] && [ "${UPDATE:-0}" != 1 ]; then printf 'utf8proc: got %s, want %s (set UPDATE=1 after review)\n' "$got" "$want" >&2; exit 1; fi
if [ "$got" != "$want" ]; then cp "$stage/utf8proc.wasm" "$here/utf8proc.wasm"; fi
printf 'utf8proc: verified %s\n' "$got"
