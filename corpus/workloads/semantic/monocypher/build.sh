#!/usr/bin/env sh
set -eu
here=$(cd "$(dirname "$0")" && pwd); root=$(cd "$here/../../../.." && pwd)
repo=https://github.com/LoupVaillant/Monocypher.git; rev=1830c06d5910fba451cec329c8f30f348fc607db
upstream="$root/.tmp/upstream/monocypher"; sdk=${WASI_SDK:-/opt/wasi-sdk}
if [ ! -x "$sdk/bin/clang" ]; then printf 'monocypher: set WASI_SDK\n' >&2; exit 1; fi
if [ ! -d "$upstream/.git" ]; then git clone --filter=blob:none --no-checkout "$repo" "$upstream"; fi
git -C "$upstream" fetch --depth=1 origin "$rev" >/dev/null 2>&1; git -C "$upstream" checkout --detach "$rev" >/dev/null 2>&1
stage=$(mktemp -d); trap 'rm -rf "$stage"' EXIT
"$sdk/bin/clang" --target=wasm32-wasip1 -O2 -DNDEBUG -nostartfiles -ffunction-sections -fdata-sections -I"$upstream/src" \
	"$upstream/src/monocypher.c" "$here/wago_monocypher.c" \
	-Wl,--no-entry -Wl,--export=monocypher_run -Wl,--export-memory -o "$stage/monocypher.wasm"
got=$(shasum -a 256 "$stage/monocypher.wasm" | awk '{print $1}'); want=$(shasum -a 256 "$here/monocypher.wasm" 2>/dev/null | awk '{print $1}')
if [ "$got" != "$want" ] && [ "${UPDATE:-0}" != 1 ]; then printf 'monocypher: got %s, want %s (set UPDATE=1 after review)\n' "$got" "$want" >&2; exit 1; fi
if [ "$got" != "$want" ]; then cp "$stage/monocypher.wasm" "$here/monocypher.wasm"; fi
printf 'monocypher: verified %s\n' "$got"
