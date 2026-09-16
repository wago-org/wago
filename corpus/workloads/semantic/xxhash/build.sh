#!/usr/bin/env sh
set -eu
here=$(cd "$(dirname "$0")" && pwd); root=$(cd "$here/../../../.." && pwd)
repo=https://github.com/Cyan4973/xxHash.git; rev=f7009014c049235e14b32af0bfe1914906a00d17
upstream="$root/.tmp/upstream/xxhash"; sdk=${WASI_SDK:-/opt/wasi-sdk}
if [ ! -x "$sdk/bin/clang" ]; then printf 'xxhash: set WASI_SDK\n' >&2; exit 1; fi
if [ ! -d "$upstream/.git" ]; then git clone --filter=blob:none --no-checkout "$repo" "$upstream"; fi
git -C "$upstream" fetch --depth=1 origin "$rev" >/dev/null 2>&1; git -C "$upstream" checkout --detach "$rev" >/dev/null 2>&1
stage=$(mktemp -d); trap 'rm -rf "$stage"' EXIT
"$sdk/bin/clang" --target=wasm32-wasip1 -O2 -DNDEBUG -nostartfiles -ffunction-sections -fdata-sections -I"$upstream" \
	"$upstream/xxhash.c" "$here/wago_xxhash.c" \
	-Wl,--no-entry -Wl,--export=xxhash_run -Wl,--export-memory -Wl,--initial-memory=262144 \
	-o "$stage/xxhash.wasm"
got=$(shasum -a 256 "$stage/xxhash.wasm" | awk '{print $1}'); want=$(shasum -a 256 "$here/xxhash.wasm" 2>/dev/null | awk '{print $1}')
if [ "$got" != "$want" ] && [ "${UPDATE:-0}" != 1 ]; then printf 'xxhash: got %s, want %s (set UPDATE=1 after review)\n' "$got" "$want" >&2; exit 1; fi
if [ "$got" != "$want" ]; then cp "$stage/xxhash.wasm" "$here/xxhash.wasm"; fi
printf 'xxhash: verified %s\n' "$got"
