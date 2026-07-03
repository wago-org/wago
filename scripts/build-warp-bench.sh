#!/usr/bin/env sh
# Build the WARP comparison harness (vb_bench) used by benchpub's -warp mode.
# warp/ is a git submodule (BMW's WARP), so the bench tweak — taking real i32
# args and timing a proper exec loop — lives as a patch in this repo
# (bench/warp/bench-main.patch) and is applied to the submodule's working tree
# here rather than committed upstream. The result is warp/build-bench/bin/vb_bench,
# the path benchpub's defaultWarpHarness expects.
#
# Self-contained: on a fresh clone this checks out the warp submodule for you, so
# `make bench-warp` works with nothing but cmake + a C++14 toolchain. The x86-64
# bench build needs none of WARP's own nested submodules (the softfloat one is
# only for the TriCore backend), so a plain, non-recursive checkout is enough.
set -eu

root=$(git rev-parse --show-toplevel)
warp="$root/warp"
patch="$root/bench/warp/bench-main.patch"

command -v cmake >/dev/null 2>&1 || { printf 'build-warp-bench: cmake not found (install cmake + a C++ compiler)\n' >&2; exit 1; }

# Fresh clone: check out the warp submodule (non-recursive — see note above).
if [ ! -d "$warp/src" ]; then
	printf 'build-warp-bench: checking out warp submodule...\n'
	git -C "$root" submodule update --init warp
fi
[ -d "$warp/src" ] || { printf 'build-warp-bench: warp submodule still missing (run: git submodule update --init warp)\n' >&2; exit 1; }

# Apply the bench patch to a pristine copy of the harness, so the build always
# reflects the committed patch — deterministic across machines and re-runs.
( cd "$warp" && git checkout -- bench/main.cpp 2>/dev/null || true; git apply "$patch" )

# Portable core count (Linux nproc, macOS/BSD sysctl, else a safe default).
jobs=$(nproc 2>/dev/null || sysctl -n hw.ncpu 2>/dev/null || echo 4)

# Build with the fair-comparison config (eager allocation on, interruption off),
# applied to the whole project so the library and harness agree.
cmake -S "$warp" -B "$warp/build-bench" \
	-DENABLE_BENCH=ON -DVB_ENABLE_DEV_FEATURE=OFF -DENABLE_UNITTEST=OFF \
	-DENABLE_SPECTEST=OFF -DENABLE_BINDING=OFF -DCMAKE_BUILD_TYPE=Release \
	-DCMAKE_CXX_FLAGS="-DINTERRUPTION_REQUEST=0 -DEAGER_ALLOCATION=1" >/dev/null
cmake --build "$warp/build-bench" --target vb_bench -j"$jobs" >/dev/null
printf 'build-warp-bench: built %s\n' "$warp/build-bench/bin/vb_bench"
