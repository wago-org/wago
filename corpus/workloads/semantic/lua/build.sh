#!/usr/bin/env sh
set -eu
here=$(cd "$(dirname "$0")" && pwd); root=$(cd "$here/../../../.." && pwd)
repo=https://github.com/lua/lua.git; rev=6e22fedb74cf0c9b6656e9fce8b7331db847c605
upstream="$root/.tmp/upstream/lua"; sdk=${WASI_SDK:-/opt/wasi-sdk}
if [ ! -x "$sdk/bin/clang" ]; then printf 'lua: set WASI_SDK\n' >&2; exit 1; fi
if [ ! -d "$upstream/.git" ]; then git clone --filter=blob:none --no-checkout "$repo" "$upstream"; fi
git -C "$upstream" fetch --depth=1 origin "$rev" >/dev/null 2>&1; git -C "$upstream" checkout --detach "$rev" >/dev/null 2>&1
stage=$(mktemp -d); trap 'rm -rf "$stage"' EXIT
core="lapi.c lcode.c lctype.c ldebug.c ldo.c ldump.c lfunc.c lgc.c llex.c lmem.c lobject.c lopcodes.c lparser.c lstate.c lstring.c ltable.c ltm.c lundump.c lvm.c lzio.c"
libs="lauxlib.c lstrlib.c"
sources=
for file in $core $libs; do sources="$sources $upstream/$file"; done
# shellcheck disable=SC2086
"$sdk/bin/clang" --target=wasm32-wasip1 -O2 -DNDEBUG -nostartfiles \
	-ffunction-sections -fdata-sections -DLUA_USE_C89 -include "$here/wago_lua_port.h" -I"$upstream" \
	$sources "$here/wago_lua.c" -lm \
	-Wl,--strip-debug -Wl,--no-entry -Wl,--export=lua_run -Wl,--export-memory \
	-o "$stage/lua.wasm"
got=$(shasum -a 256 "$stage/lua.wasm" | awk '{print $1}'); want=$(shasum -a 256 "$here/lua.wasm" 2>/dev/null | awk '{print $1}')
if [ "$got" != "$want" ] && [ "${UPDATE:-0}" != 1 ]; then printf 'lua: got %s, want %s (set UPDATE=1 after review)\n' "$got" "$want" >&2; exit 1; fi
if [ "$got" != "$want" ]; then cp "$stage/lua.wasm" "$here/lua.wasm"; fi
printf 'lua: verified %s\n' "$got"
