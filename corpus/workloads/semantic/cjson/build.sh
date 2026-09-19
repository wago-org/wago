#!/usr/bin/env sh
set -eu
here=$(cd "$(dirname "$0")" && pwd); root=$(cd "$here/../../../.." && pwd)
repo=https://github.com/DaveGamble/cJSON.git; rev=6d9f2443ab071f86e5d9b43025a40929ec41c46c
upstream="$root/.tmp/upstream/cjson"; sdk=${WASI_SDK:-/opt/wasi-sdk}
if [ ! -x "$sdk/bin/clang" ]; then printf 'cjson: set WASI_SDK\n' >&2; exit 1; fi
if [ ! -d "$upstream/.git" ]; then git clone --filter=blob:none --no-checkout "$repo" "$upstream"; fi
git -C "$upstream" fetch --depth=1 origin "$rev" >/dev/null 2>&1; git -C "$upstream" checkout --detach "$rev" >/dev/null 2>&1
stage=$(mktemp -d); trap 'rm -rf "$stage"' EXIT
"$sdk/bin/clang" --target=wasm32-wasip1 -O2 -DNDEBUG -nostartfiles -ffunction-sections -fdata-sections -I"$upstream" \
	"$upstream/cJSON.c" "$here/wago_cjson.c" -lm \
	-Wl,--strip-debug -Wl,--no-entry -Wl,--export=cjson_run -Wl,--export-memory -o "$stage/cjson.wasm"
got=$(shasum -a 256 "$stage/cjson.wasm" | awk '{print $1}'); want=$(shasum -a 256 "$here/cjson.wasm" 2>/dev/null | awk '{print $1}')
if [ "$got" != "$want" ] && [ "${UPDATE:-0}" != 1 ]; then printf 'cjson: got %s, want %s (set UPDATE=1 after review)\n' "$got" "$want" >&2; exit 1; fi
if [ "$got" != "$want" ]; then cp "$stage/cjson.wasm" "$here/cjson.wasm"; fi
printf 'cjson: verified %s\n' "$got"
