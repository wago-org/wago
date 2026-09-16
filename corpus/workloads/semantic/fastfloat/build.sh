#!/usr/bin/env sh
set -eu
here=$(cd "$(dirname "$0")" && pwd); root=$(cd "$here/../../../.." && pwd)
repo=https://github.com/fastfloat/fast_float.git; rev=b0ab987b3dfdde13fa1915f65ef2a5c068d9208c
upstream="$root/.tmp/upstream/fast_float"; sdk=${WASI_SDK:-/opt/wasi-sdk}
if [ ! -x "$sdk/bin/clang++" ]; then printf 'fastfloat: set WASI_SDK\n' >&2; exit 1; fi
if [ ! -d "$upstream/.git" ]; then git clone --filter=blob:none --no-checkout "$repo" "$upstream"; fi
git -C "$upstream" fetch --depth=1 origin "$rev" >/dev/null 2>&1; git -C "$upstream" checkout --detach "$rev" >/dev/null 2>&1
stage=$(mktemp -d); trap 'rm -rf "$stage"' EXIT
"$sdk/bin/clang++" --target=wasm32-wasip1 -std=c++17 -O2 -DNDEBUG -nostartfiles -ffunction-sections -fdata-sections -I"$upstream/include" \
	"$here/wago_fastfloat.cpp" -Wl,--no-entry -Wl,--export=fastfloat_run -Wl,--export-memory -o "$stage/fastfloat.wasm"
got=$(shasum -a 256 "$stage/fastfloat.wasm" | awk '{print $1}'); want=$(shasum -a 256 "$here/fastfloat.wasm" 2>/dev/null | awk '{print $1}')
if [ "$got" != "$want" ] && [ "${UPDATE:-0}" != 1 ]; then printf 'fastfloat: got %s, want %s (set UPDATE=1 after review)\n' "$got" "$want" >&2; exit 1; fi
if [ "$got" != "$want" ]; then cp "$stage/fastfloat.wasm" "$here/fastfloat.wasm"; fi
printf 'fastfloat: verified %s\n' "$got"
