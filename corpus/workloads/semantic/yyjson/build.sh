#!/usr/bin/env sh
set -eu

here=$(cd "$(dirname "$0")" && pwd)
root=$(cd "$here/../../../.." && pwd)
repo=https://github.com/ibireme/yyjson.git
rev=6447536015f3d600f3d65323b10976103b337ca7
upstream="$root/.tmp/upstream/yyjson"
sdk=${WASI_SDK:-/opt/wasi-sdk}

if [ ! -x "$sdk/bin/clang" ]; then
	printf 'yyjson: wasi-sdk clang not found at %s/bin/clang (set WASI_SDK)\n' "$sdk" >&2
	exit 1
fi
if [ ! -d "$upstream/.git" ]; then
	git clone --filter=blob:none --no-checkout "$repo" "$upstream"
fi
git -C "$upstream" fetch --depth=1 origin "$rev" >/dev/null 2>&1
git -C "$upstream" checkout --detach "$rev" >/dev/null 2>&1

stage=$(mktemp -d)
trap 'rm -rf "$stage"' EXIT
"$sdk/bin/clang" --target=wasm32-wasip1 -O2 -DNDEBUG -nostartfiles -ffunction-sections -fdata-sections \
	-I"$upstream/src" "$upstream/src/yyjson.c" "$here/wago_yyjson.c" \
	-Wl,--strip-debug -Wl,--no-entry -Wl,--export=yyjson_run -Wl,--export-memory \
	-o "$stage/yyjson.wasm"

got=$(shasum -a 256 "$stage/yyjson.wasm" | awk '{print $1}')
want=$(shasum -a 256 "$here/yyjson.wasm" 2>/dev/null | awk '{print $1}')
if [ "$got" != "$want" ] && [ "${UPDATE:-0}" != 1 ]; then
	printf 'yyjson: rebuilt artifact differs or is absent\n  got  %s\n  want %s\nset UPDATE=1 after reviewing the source and flags\n' "$got" "$want" >&2
	exit 1
fi
if [ "$got" != "$want" ]; then cp "$stage/yyjson.wasm" "$here/yyjson.wasm"; fi
printf 'yyjson: verified %s\n' "$got"
