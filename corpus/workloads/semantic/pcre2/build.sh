#!/usr/bin/env sh
set -eu
here=$(cd "$(dirname "$0")" && pwd); root=$(cd "$here/../../../.." && pwd)
repo=https://github.com/PCRE2Project/pcre2.git; rev=085f8170c27b8336f0bd61f8e99518717ad0950d
upstream="$root/.tmp/upstream/pcre2"; sdk=${WASI_SDK:-/opt/wasi-sdk}
if [ ! -x "$sdk/bin/clang" ]; then printf 'pcre2: set WASI_SDK\n' >&2; exit 1; fi
if [ ! -d "$upstream/.git" ]; then git clone --filter=blob:none --no-checkout "$repo" "$upstream"; fi
git -C "$upstream" fetch --depth=1 origin "$rev" >/dev/null 2>&1; git -C "$upstream" checkout --detach "$rev" >/dev/null 2>&1
stage=$(mktemp -d); trap 'rm -rf "$stage"' EXIT
cp "$upstream/src/config.h.generic" "$stage/config.h"; cp "$upstream/src/pcre2.h.generic" "$stage/pcre2.h"; cp "$upstream/src/pcre2_chartables.c.dist" "$stage/pcre2_chartables.c"
"$sdk/bin/clang" --target=wasm32-wasip1 -O2 -DNDEBUG -DHAVE_CONFIG_H -DSUPPORT_PCRE2_8 -DSUPPORT_UNICODE -DPCRE2_CODE_UNIT_WIDTH=8 \
	-nostartfiles -ffunction-sections -fdata-sections -I"$stage" -I"$upstream/src" \
	"$upstream/src/pcre2_auto_possess.c" "$upstream/src/pcre2_chkdint.c" "$stage/pcre2_chartables.c" \
	"$upstream/src/pcre2_compile.c" "$upstream/src/pcre2_compile_cgroup.c" "$upstream/src/pcre2_compile_class.c" \
	"$upstream/src/pcre2_config.c" "$upstream/src/pcre2_context.c" "$upstream/src/pcre2_convert.c" \
	"$upstream/src/pcre2_dfa_match.c" "$upstream/src/pcre2_error.c" "$upstream/src/pcre2_extuni.c" \
	"$upstream/src/pcre2_find_bracket.c" "$upstream/src/pcre2_jit_compile.c" "$upstream/src/pcre2_maketables.c" \
	"$upstream/src/pcre2_match.c" "$upstream/src/pcre2_match_data.c" "$upstream/src/pcre2_match_next.c" \
	"$upstream/src/pcre2_newline.c" "$upstream/src/pcre2_ord2utf.c" "$upstream/src/pcre2_pattern_info.c" \
	"$upstream/src/pcre2_script_run.c" "$upstream/src/pcre2_serialize.c" "$upstream/src/pcre2_string_utils.c" \
	"$upstream/src/pcre2_study.c" "$upstream/src/pcre2_substitute.c" "$upstream/src/pcre2_substring.c" \
	"$upstream/src/pcre2_tables.c" "$upstream/src/pcre2_ucd.c" "$upstream/src/pcre2_valid_utf.c" "$upstream/src/pcre2_xclass.c" \
	"$here/wago_pcre2.c" -Wl,--strip-debug -Wl,--no-entry -Wl,--export=pcre2_run -Wl,--export-memory -o "$stage/pcre2.wasm"
got=$(shasum -a 256 "$stage/pcre2.wasm" | awk '{print $1}'); want=$(shasum -a 256 "$here/pcre2.wasm" 2>/dev/null | awk '{print $1}')
if [ "$got" != "$want" ] && [ "${UPDATE:-0}" != 1 ]; then printf 'pcre2: got %s, want %s (set UPDATE=1 after review)\n' "$got" "$want" >&2; exit 1; fi
if [ "$got" != "$want" ]; then cp "$stage/pcre2.wasm" "$here/pcre2.wasm"; fi
printf 'pcre2: verified %s\n' "$got"
