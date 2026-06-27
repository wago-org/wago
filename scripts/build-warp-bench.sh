#!/usr/bin/env sh
# Build the WARP comparison harness (vb_bench) used by benchpub's -warp mode.
# warp/ is a git submodule (BMW's WARP), so the bench tweak — taking real i32
# args and timing a proper exec loop — lives as a patch in this repo
# (bench/warp/bench-main.patch) and is applied to the submodule's working tree
# here rather than committed upstream. The result is warp/build-bench/bin/vb_bench,
# the path benchpub's defaultWarpHarness expects. Needs cmake + a C++ compiler.
set -eu

root=$(git rev-parse --show-toplevel)
warp="$root/warp"
patch="$root/bench/warp/bench-main.patch"

[ -d "$warp/src" ] || { printf 'build-warp-bench: warp submodule not checked out (git submodule update --init warp)\n' >&2; exit 1; }
command -v cmake >/dev/null 2>&1 || { printf 'build-warp-bench: cmake not found\n' >&2; exit 1; }

# Apply the bench patch unless it's already in place (idempotent).
if ! grep -q "exec_ns=" "$warp/bench/main.cpp" 2>/dev/null; then
	printf 'build-warp-bench: applying bench patch...\n'
	( cd "$warp" && git apply "$patch" )
fi

# Build with the fair-comparison config (eager allocation on, interruption off),
# applied to the whole project so the library and harness agree.
cmake -S "$warp" -B "$warp/build-bench" \
	-DENABLE_BENCH=ON -DVB_ENABLE_DEV_FEATURE=OFF -DENABLE_UNITTEST=OFF \
	-DENABLE_SPECTEST=OFF -DENABLE_BINDING=OFF -DCMAKE_BUILD_TYPE=Release \
	-DCMAKE_CXX_FLAGS="-DINTERRUPTION_REQUEST=0 -DEAGER_ALLOCATION=1" >/dev/null
cmake --build "$warp/build-bench" --target vb_bench -j"$(nproc)" >/dev/null
printf 'build-warp-bench: built %s\n' "$warp/build-bench/bin/vb_bench"
