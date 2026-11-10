#!/usr/bin/env sh
set -eu
here=$(cd "$(dirname "$0")" && pwd); root=$(cd "$here/../../../.." && pwd)
repo=https://github.com/lvandeve/lodepng.git; rev=ed6fe5825c6a4fbb7f58ab35a4231c7543cd452a
upstream="$root/.tmp/upstream/lodepng"; sdk=${WASI_SDK:-/opt/wasi-sdk}
if [ ! -x "$sdk/bin/clang++" ]; then printf 'lodepng: set WASI_SDK\n' >&2; exit 1; fi
if [ ! -d "$upstream/.git" ]; then git clone --filter=blob:none --no-checkout "$repo" "$upstream"; fi
git -C "$upstream" fetch --depth=1 origin "$rev" >/dev/null 2>&1; git -C "$upstream" checkout --detach "$rev" >/dev/null 2>&1
stage=$(mktemp -d); trap 'rm -rf "$stage"' EXIT
"$sdk/bin/clang++" --target=wasm32-wasip1 -O2 -DNDEBUG -nostartfiles -ffunction-sections -fdata-sections -I"$upstream" \
	"$upstream/lodepng.cpp" "$here/wago_lodepng.cpp" \
	-Wl,--strip-debug -Wl,--no-entry -Wl,--export=lodepng_run -Wl,--export-memory -o "$stage/lodepng.wasm"
got=$(shasum -a 256 "$stage/lodepng.wasm" | awk '{print $1}'); want=$(shasum -a 256 "$here/lodepng.wasm" 2>/dev/null | awk '{print $1}')
if [ "$got" != "$want" ] && [ "${UPDATE:-0}" != 1 ]; then printf 'lodepng: got %s, want %s (set UPDATE=1 after review)\n' "$got" "$want" >&2; exit 1; fi
if [ "$got" != "$want" ]; then cp "$stage/lodepng.wasm" "$here/lodepng.wasm"; fi
printf 'lodepng: verified %s\n' "$got"
