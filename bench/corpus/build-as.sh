#!/usr/bin/env sh
# Rebuild the AssemblyScript real-world corpus modules from the source AS
# libraries and copy the resulting .wasm into corpus/. The .wasm are committed
# so the benchmark suite needs no AS toolchain at run time; rerun this only when
# the libraries or their assembly/wago-bench.ts entry points change.
#
# Each library exposes a host-driven bench module at assembly/wago-bench.ts:
# tight loops the Go host times, taking an i32 count, returning an i32 so the
# work isn't dead-code eliminated. wago has no wasm start section, so the module
# is built with --exportStart _initialize (the host calls it once after
# instantiate; see the manifest's "init"). --disable simd keeps them on wago's
# core-1.0 backend.
#
# Point AS_ROOT at the parent dir holding json-as/, blake-as/, utf-as/
# (default: ~/Code/AssemblyScript). A library that isn't present is skipped.
set -eu

here=$(cd "$(dirname "$0")" && pwd) # bench/corpus
as_root="${AS_ROOT:-$HOME/Code/AssemblyScript}"

# asc from whichever library's node_modules is available; overridable via ASC.
asc_bin=""
find_asc() {
	[ -n "${ASC:-}" ] && { asc_bin="$ASC"; return; }
	for lib in json-as blake-as utf-as; do
		if [ -x "$as_root/$lib/node_modules/.bin/asc" ]; then
			asc_bin="$as_root/$lib/node_modules/.bin/asc"
			return
		fi
	done
	command -v asc >/dev/null 2>&1 && asc_bin=asc
}

# build <lib> <entry-relative-to-lib> <out-name> [extra asc args...]
build() {
	lib=$1 entry=$2 out=$3
	shift 3
	dir="$as_root/$lib"
	if [ ! -d "$dir" ]; then
		printf 'build-as: %s not found at %s; skipping\n' "$lib" "$dir" >&2
		return
	fi
	printf 'build-as: %s -> %s.wasm\n' "$lib" "$out"
	# JSON_MODE selects json-as's SWAR codegen in its @json transform; harmless
	# (unset) for the other libraries.
	( cd "$dir" && JSON_MODE="${JSON_MODE:-SWAR}" "$asc_bin" "$entry" -o "build/$out.wasm" \
		-O3 --noAssert --uncheckedBehavior always \
		--exportStart _initialize --disable simd --enable bulk-memory "$@" )
	cp "$dir/build/$out.wasm" "$here/$out.wasm"
}

find_asc
[ -n "$asc_bin" ] || { printf 'build-as: asc not found (set ASC or install AS in a library)\n' >&2; exit 1; }
printf 'build-as: using asc = %s\n' "$asc_bin"

# Focused local fixtures preserve the source shape of third-party idioms without
# requiring the whole package checkout. They use the same pinned asc selected
# above and are committed beside the larger corpus artifacts.
build_local() { # source output
	source=$1 out=$2
	printf 'build-as: %s -> %s.wasm\n' "$source" "$out"
	"$asc_bin" "$here/as/$source" -o "$here/$out.wasm" \
		-O3 --noAssert --uncheckedBehavior always --runtime stub \
		--disable simd --enable bulk-memory
}

# json-as needs its @json transform and the incremental runtime (it allocates +
# GCs); blake-as/utf-as are allocation-free, so the leaner stub runtime is used.
if [ "${SIMD_ONLY:-0}" != 1 ]; then
	build json-as  assembly/wago-bench.ts json-as  --transform ./transform --runtime incremental --exportRuntime
	build blake-as assembly/wago-bench.ts blake-as --runtime stub
	build utf-as   assembly/wago-bench.ts utf-as   --runtime stub
	build_local xjb-mulhi.ts xjb-mulhi
fi

# SIMD twins use checked-in Wago entrypoints so the corpus is reproducible.
build_simd() { # library corpus-source output [extra asc args...]
	lib=$1 source=$2 out=$3
	shift 3
	dir="$as_root/$lib"
	if [ ! -d "$dir" ]; then
		printf 'build-as: %s not found at %s; skipping\n' "$lib" "$dir" >&2
		return
	fi
	cp "$here/as/$source" "$dir/assembly/wago-bench.ts"
	printf 'build-as: %s SIMD -> %s.wasm\n' "$lib" "$out"
	( cd "$dir" && JSON_MODE=SIMD "$asc_bin" assembly/wago-bench.ts -o "build/$out.wasm" \
		-O3 --noAssert --uncheckedBehavior always --exportStart _initialize --enable simd --enable bulk-memory "$@" )
	cp "$dir/build/$out.wasm" "$here/$out.wasm"
}

build_simd json-as json-as-simd.ts json-as-simd --transform ./transform --runtime incremental --exportRuntime
build_simd blake-as blake-as-simd.ts blake-as-simd --runtime stub
build_simd utf-as utf-as-simd.ts utf-as-simd --runtime stub

printf 'build-as: done\n'
