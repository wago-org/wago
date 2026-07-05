#!/usr/bin/env sh
# Build the wasm3 comparison harness (wasm3_bench) used by benchpub's -wasm3 mode.
# wasm3/ is a git submodule (the wasm3 interpreter); the harness that times an
# end-to-end WASI run lives in this repo (bench/wasm3/harness.c) and is compiled
# against the submodule's amalgamated C sources. The result is
# bench/wasm3/wasm3_bench, the path benchpub's defaultWasm3Harness expects.
#
# Self-contained: on a fresh clone this checks out the wasm3 submodule for you,
# so `make bench-wasm3` works with nothing but a C compiler (no cmake needed).
set -eu

root=$(git rev-parse --show-toplevel)
wasm3="$root/wasm3"
out="$root/bench/wasm3/wasm3_bench"

CC=${CC:-cc}
command -v "$CC" >/dev/null 2>&1 || { printf 'build-wasm3-bench: C compiler not found (set CC or install cc)\n' >&2; exit 1; }

# Fresh clone: check out the wasm3 submodule.
if [ ! -d "$wasm3/source" ]; then
	printf 'build-wasm3-bench: checking out wasm3 submodule...\n'
	git -C "$root" submodule update --init wasm3
fi
[ -d "$wasm3/source" ] || { printf 'build-wasm3-bench: wasm3 submodule still missing (run: git submodule update --init wasm3)\n' >&2; exit 1; }

# d_m3HasWASI links the WASI host functions the corpus programs import. The
# amalgamated build is just source/*.c plus our harness (which provides main).
# shellcheck disable=SC2086
"$CC" -O2 -Wall -I"$wasm3/source" -Dd_m3HasWASI \
	-o "$out" \
	"$wasm3"/source/*.c "$root/bench/wasm3/harness.c" -lm

printf 'build-wasm3-bench: built %s\n' "$out"
