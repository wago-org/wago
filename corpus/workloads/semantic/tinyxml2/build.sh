#!/usr/bin/env sh
set -eu
here=$(cd "$(dirname "$0")" && pwd); root=$(cd "$here/../../../.." && pwd)
repo=https://github.com/leethomason/tinyxml2.git; rev=8224e427b655b83dae5e2298f1e6919523a78737
upstream="$root/.tmp/upstream/tinyxml2"; sdk=${WASI_SDK:-/opt/wasi-sdk}
if [ ! -x "$sdk/bin/clang++" ]; then printf 'tinyxml2: set WASI_SDK\n' >&2; exit 1; fi
if [ ! -d "$upstream/.git" ]; then git clone --filter=blob:none --no-checkout "$repo" "$upstream"; fi
git -C "$upstream" fetch --depth=1 origin "$rev" >/dev/null 2>&1; git -C "$upstream" checkout --detach "$rev" >/dev/null 2>&1
stage=$(mktemp -d); trap 'rm -rf "$stage"' EXIT
"$sdk/bin/clang++" --target=wasm32-wasip1 -O2 -DNDEBUG -nostartfiles -ffunction-sections -fdata-sections -fno-exceptions -I"$upstream" \
	"$upstream/tinyxml2.cpp" "$here/wago_tinyxml2.cpp" \
	-Wl,--strip-debug -Wl,--no-entry -Wl,--export=tinyxml2_run -Wl,--export-memory \
	-o "$stage/tinyxml2.wasm"
got=$(shasum -a 256 "$stage/tinyxml2.wasm" | awk '{print $1}'); want=$(shasum -a 256 "$here/tinyxml2.wasm" 2>/dev/null | awk '{print $1}')
if [ "$got" != "$want" ] && [ "${UPDATE:-0}" != 1 ]; then printf 'tinyxml2: got %s, want %s (set UPDATE=1 after review)\n' "$got" "$want" >&2; exit 1; fi
if [ "$got" != "$want" ]; then cp "$stage/tinyxml2.wasm" "$here/tinyxml2.wasm"; fi
printf 'tinyxml2: verified %s\n' "$got"
