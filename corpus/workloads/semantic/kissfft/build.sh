#!/usr/bin/env sh
set -eu
here=$(cd "$(dirname "$0")" && pwd); root=$(cd "$here/../../../.." && pwd)
repo=https://github.com/mborgerding/kissfft.git; rev=e5e3fac46e0d94a8f8170c06706b7a4218828333
upstream="$root/.tmp/upstream/kissfft"; sdk=${WASI_SDK:-/opt/wasi-sdk}
if [ ! -x "$sdk/bin/clang" ]; then printf 'kissfft: set WASI_SDK\n' >&2; exit 1; fi
if [ ! -d "$upstream/.git" ]; then git clone --filter=blob:none --no-checkout "$repo" "$upstream"; fi
git -C "$upstream" fetch --depth=1 origin "$rev" >/dev/null 2>&1; git -C "$upstream" checkout --detach "$rev" >/dev/null 2>&1
stage=$(mktemp -d); trap 'rm -rf "$stage"' EXIT
"$sdk/bin/clang" --target=wasm32-wasip1 -O2 -DNDEBUG -nostartfiles -ffunction-sections -fdata-sections -I"$upstream" \
	"$upstream/kiss_fft.c" "$here/wago_kissfft.c" -lm \
	-Wl,--no-entry -Wl,--export=kissfft_run -Wl,--export-memory \
	-o "$stage/kissfft.wasm"
got=$(shasum -a 256 "$stage/kissfft.wasm" | awk '{print $1}'); want=$(shasum -a 256 "$here/kissfft.wasm" 2>/dev/null | awk '{print $1}')
if [ "$got" != "$want" ] && [ "${UPDATE:-0}" != 1 ]; then printf 'kissfft: got %s, want %s (set UPDATE=1 after review)\n' "$got" "$want" >&2; exit 1; fi
if [ "$got" != "$want" ]; then cp "$stage/kissfft.wasm" "$here/kissfft.wasm"; fi
printf 'kissfft: verified %s\n' "$got"
