#!/usr/bin/env sh
set -eu
here=$(cd "$(dirname "$0")" && pwd); root=$(cd "$here/../../../.." && pwd)
repo=https://github.com/memononen/nanosvg.git; rev=239e102ec2c691f2902e20ace2ed36ee4a35cfe6
upstream="$root/.tmp/upstream/nanosvg"; sdk=${WASI_SDK:-/opt/wasi-sdk}
if [ ! -x "$sdk/bin/clang" ]; then printf 'nanosvg: set WASI_SDK\n' >&2; exit 1; fi
if [ ! -d "$upstream/.git" ]; then git clone --filter=blob:none --no-checkout "$repo" "$upstream"; fi
git -C "$upstream" fetch --depth=1 origin "$rev" >/dev/null 2>&1; git -C "$upstream" checkout --detach "$rev" >/dev/null 2>&1
stage=$(mktemp -d); trap 'rm -rf "$stage"' EXIT
"$sdk/bin/clang" --target=wasm32-wasip1 -O2 -DNDEBUG -nostartfiles -ffunction-sections -fdata-sections -I"$upstream/src" \
	"$here/wago_nanosvg.c" -lm \
	-Wl,--no-entry -Wl,--export=nanosvg_run -Wl,--export-memory \
	-o "$stage/nanosvg.wasm"
got=$(shasum -a 256 "$stage/nanosvg.wasm" | awk '{print $1}'); want=$(shasum -a 256 "$here/nanosvg.wasm" 2>/dev/null | awk '{print $1}')
if [ "$got" != "$want" ] && [ "${UPDATE:-0}" != 1 ]; then printf 'nanosvg: got %s, want %s (set UPDATE=1 after review)\n' "$got" "$want" >&2; exit 1; fi
if [ "$got" != "$want" ]; then cp "$stage/nanosvg.wasm" "$here/nanosvg.wasm"; fi
printf 'nanosvg: verified %s\n' "$got"
