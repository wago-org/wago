#!/usr/bin/env sh
set -eu
here=$(cd "$(dirname "$0")" && pwd); root=$(cd "$here/../../../.." && pwd)
repo=https://github.com/mackron/dr_libs.git; rev=dfe8377631000664666519fdb83da193fd8037f4
upstream="$root/.tmp/upstream/dr_libs"; sdk=${WASI_SDK:-/opt/wasi-sdk}
if [ ! -x "$sdk/bin/clang" ]; then printf 'drwav: set WASI_SDK\n' >&2; exit 1; fi
if [ ! -d "$upstream/.git" ]; then git clone --filter=blob:none --no-checkout "$repo" "$upstream"; fi
git -C "$upstream" fetch --depth=1 origin "$rev" >/dev/null 2>&1; git -C "$upstream" checkout --detach "$rev" >/dev/null 2>&1
stage=$(mktemp -d); trap 'rm -rf "$stage"' EXIT
"$sdk/bin/clang" --target=wasm32-wasip1 -O2 -DNDEBUG -nostartfiles -ffunction-sections -fdata-sections -I"$upstream" \
	"$here/wago_drwav.c" -lm -Wl,--no-entry -Wl,--export=drwav_run -Wl,--export-memory -o "$stage/drwav.wasm"
got=$(shasum -a 256 "$stage/drwav.wasm" | awk '{print $1}'); want=$(shasum -a 256 "$here/drwav.wasm" 2>/dev/null | awk '{print $1}')
if [ "$got" != "$want" ] && [ "${UPDATE:-0}" != 1 ]; then printf 'drwav: got %s, want %s (set UPDATE=1 after review)\n' "$got" "$want" >&2; exit 1; fi
if [ "$got" != "$want" ]; then cp "$stage/drwav.wasm" "$here/drwav.wasm"; fi
printf 'drwav: verified %s\n' "$got"
