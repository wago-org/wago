#!/usr/bin/env sh
set -eu
here=$(cd "$(dirname "$0")" && pwd); root=$(cd "$here/../../../.." && pwd)
repo=https://github.com/wren-lang/wren.git; rev=99d2f0b8fc2686134b32b18166e037639f7e9f2c
upstream="$root/.tmp/upstream/wren"; sdk=${WASI_SDK:-/opt/wasi-sdk}
if [ ! -x "$sdk/bin/clang" ]; then printf 'wren: set WASI_SDK\n' >&2; exit 1; fi
if [ ! -d "$upstream/.git" ]; then git clone --filter=blob:none --no-checkout "$repo" "$upstream"; fi
git -C "$upstream" fetch --depth=1 origin "$rev" >/dev/null 2>&1; git -C "$upstream" checkout --detach "$rev" >/dev/null 2>&1
stage=$(mktemp -d); trap 'rm -rf "$stage"' EXIT
"$sdk/bin/clang" --target=wasm32-wasip1 -O2 -DNDEBUG -Wno-deprecated-declarations -DWREN_OPT_META=0 -DWREN_OPT_RANDOM=0 -Dclock=wago_clock -nostartfiles -ffunction-sections -fdata-sections \
	-I"$upstream/src/include" -I"$upstream/src/vm" "$upstream"/src/vm/*.c "$here/wago_wren.c" -lm \
	-Wl,--strip-debug -Wl,--no-entry -Wl,--export=wren_run -Wl,--export=wren_modulo_run -Wl,--export-memory -o "$stage/wren.wasm"
got=$(shasum -a 256 "$stage/wren.wasm" | awk '{print $1}'); want=$(shasum -a 256 "$here/wren.wasm" 2>/dev/null | awk '{print $1}')
if [ "$got" != "$want" ] && [ "${UPDATE:-0}" != 1 ]; then printf 'wren: got %s, want %s (set UPDATE=1 after review)\n' "$got" "$want" >&2; exit 1; fi
if [ "$got" != "$want" ]; then cp "$stage/wren.wasm" "$here/wren.wasm"; fi
printf 'wren: verified %s\n' "$got"
