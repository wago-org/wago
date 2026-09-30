#!/usr/bin/env sh
set -eu
here=$(cd "$(dirname "$0")" && pwd); root=$(cd "$here/../../../.." && pwd)
repo=https://github.com/richgel999/miniz.git; rev=77d0dce8627735138c51770d1799a1ef48f2117d
upstream="$root/.tmp/upstream/miniz"; sdk=${WASI_SDK:-/opt/wasi-sdk}
if [ ! -x "$sdk/bin/clang" ]; then printf 'miniz: set WASI_SDK\n' >&2; exit 1; fi
if [ ! -d "$upstream/.git" ]; then git clone --filter=blob:none --no-checkout "$repo" "$upstream"; fi
git -C "$upstream" fetch --depth=1 origin "$rev" >/dev/null 2>&1; git -C "$upstream" checkout --detach "$rev" >/dev/null 2>&1
stage=$(mktemp -d); trap 'rm -rf "$stage"' EXIT
"$sdk/bin/clang" --target=wasm32-wasip1 -O2 -DNDEBUG -DMINIZ_NO_ARCHIVE_APIS -nostartfiles -ffunction-sections -fdata-sections -I"$here" -I"$upstream" \
	"$upstream/miniz.c" "$upstream/miniz_tdef.c" "$upstream/miniz_tinfl.c" "$here/wago_miniz.c" \
	-Wl,--strip-debug -Wl,--no-entry -Wl,--export=miniz_run -Wl,--export-memory -Wl,--initial-memory=1048576 -o "$stage/miniz.wasm"
got=$(shasum -a 256 "$stage/miniz.wasm" | awk '{print $1}'); want=$(shasum -a 256 "$here/miniz.wasm" 2>/dev/null | awk '{print $1}')
if [ "$got" != "$want" ] && [ "${UPDATE:-0}" != 1 ]; then printf 'miniz: got %s, want %s (set UPDATE=1 after review)\n' "$got" "$want" >&2; exit 1; fi
if [ "$got" != "$want" ]; then cp "$stage/miniz.wasm" "$here/miniz.wasm"; fi
printf 'miniz: verified %s\n' "$got"
