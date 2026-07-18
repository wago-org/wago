#!/bin/sh
# Reproducible linux/riscv64 qualification gate.
#
# Usage:
#   scripts/riscv64-qualify.sh qemu   # cross-build and execute under qemu-user
#   scripts/riscv64-qualify.sh native # execute/stress/benchmark on real RV64
#
# Optional environment:
#   GO, QEMU_RISCV64, OUT_DIR, STRESS_COUNT, BENCH_TIME, BENCH_COUNT,
#   BENCH_REGEX, WAGO_SPECTEST_DIR, WAST2JSON_DIR.
set -eu

root=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
cd "$root"

mode=${1:-qemu}
go_cmd=${GO:-go}
out=${OUT_DIR:-$root/.validation/riscv64-qualify}
stress_count=${STRESS_COUNT:-20}
bench_time=${BENCH_TIME:-250ms}
bench_count=${BENCH_COUNT:-5}
bench_regex=${BENCH_REGEX:-'BenchmarkExec/(isa_simd|isa_bulk_mem|memory_tree|fannkuch|nbody)'}
mkdir -p "$out"

record_host() {
	{
		date -u '+date=%Y-%m-%dT%H:%M:%SZ'
		printf 'commit=%s\n' "$(git rev-parse HEAD)"
		printf 'go=%s\n' "$($go_cmd version)"
		printf 'uname=%s\n' "$(uname -a)"
		printf 'machine=%s\n' "$(uname -m)"
	} >"$out/host.txt"
	if [ -r /proc/cpuinfo ]; then
		cp /proc/cpuinfo "$out/cpuinfo.txt"
	fi
}

build_rv64_tests() {
	GOOS=linux GOARCH=riscv64 CGO_ENABLED=0 "$go_cmd" test -c -o "$out/backend.test" ./src/core/compiler/backend/railshot/riscv64
	GOOS=linux GOARCH=riscv64 CGO_ENABLED=0 "$go_cmd" test -c -tags wago_guardpage -o "$out/backend-guard.test" ./src/core/compiler/backend/railshot/riscv64
	GOOS=linux GOARCH=riscv64 CGO_ENABLED=0 "$go_cmd" test -c -o "$out/runtime.test" ./src/core/runtime
	GOOS=linux GOARCH=riscv64 CGO_ENABLED=0 "$go_cmd" test -c -tags wago_guardpage -o "$out/runtime-guard.test" ./src/core/runtime
	GOOS=linux GOARCH=riscv64 CGO_ENABLED=0 "$go_cmd" test -c -o "$out/spike.test" ./src/core/runtime/riscv64spike
	GOOS=linux GOARCH=riscv64 CGO_ENABLED=0 "$go_cmd" test -c -o "$out/wago.test" ./src/wago
	GOOS=linux GOARCH=riscv64 CGO_ENABLED=0 "$go_cmd" test -c -tags wago_guardpage -o "$out/wago-guard.test" ./src/wago
	GOOS=linux GOARCH=riscv64 CGO_ENABLED=0 "$go_cmd" build ./...
	GOOS=linux GOARCH=riscv64 CGO_ENABLED=0 "$go_cmd" build -tags wago_guardpage ./...
	GOOS=linux GOARCH=riscv64 CGO_ENABLED=0 "$go_cmd" vet ./src/core/compiler/backend/railshot/riscv64 ./src/core/compiler/wasm ./src/core/runtime ./src/wago
}

run_specs_qemu() {
	qemu=$1
	cpu=$2
	wago_bin=$3
	label=$4
	[ -n "${WAGO_SPECTEST_DIR:-}" ] || return 0
	wast2json=${WAST2JSON_DIR:-}
	if [ -n "$wast2json" ]; then
		PATH="$wast2json:$PATH"
		export PATH
	fi
	command -v wast2json >/dev/null 2>&1 || {
		echo "WAGO_SPECTEST_DIR is set but wast2json is unavailable" >&2
		exit 1
	}
	for version in simd relaxed-simd; do
		(
			cd "$root/src/wago"
			WAGO_SPECTEST_DIR="$WAGO_SPECTEST_DIR" WAGO_SPEC_VERSION="$version" \
				"$qemu" -cpu "$cpu" "$wago_bin" -test.run '^TestSpecSuiteExec$' -test.v
		) >"$out/spec-$label-$version.log" 2>&1
		grep 'TOTAL\|^PASS$' "$out/spec-$label-$version.log" | tail -2
	done
}

run_qemu() {
	qemu=${QEMU_RISCV64:-qemu-riscv64}
	command -v "$qemu" >/dev/null 2>&1 || {
		echo "qemu-riscv64 is required" >&2
		exit 1
	}
	build_rv64_tests
	baseline='rv64,v=false,c=false,zba=false,zbb=false,zbc=false,zbs=false,zicond=false,zfa=false,zfh=false,zacas=false'

	"$qemu" -cpu "$baseline" "$out/backend.test"
	"$qemu" -cpu "$baseline" "$out/backend-guard.test"
	"$qemu" -cpu "$baseline" "$out/runtime.test"
	"$qemu" -cpu "$baseline" "$out/runtime-guard.test"
	"$qemu" -cpu "$baseline" "$out/spike.test"
	(cd "$root/src/wago" && "$qemu" -cpu "$baseline" "$out/wago.test")
	(cd "$root/src/wago" && "$qemu" -cpu "$baseline" "$out/wago-guard.test")

	# The default rv64 model exposes ratified V: detector and executable encoder
	# tests must take the positive path as well as the baseline skip path above.
	"$qemu" -cpu rv64 "$out/runtime.test" -test.run 'RVV|HWCAP|Misaligned' -test.v >"$out/rvv-detect.log"
	"$qemu" -cpu rv64 "$out/spike.test" -test.run '^TestRVVExecuteByteAdd$' -test.v >"$out/rvv-exec.log"

	run_specs_qemu "$qemu" "$baseline" "$out/wago.test" explicit
	run_specs_qemu "$qemu" "$baseline" "$out/wago-guard.test" guard
}

run_native() {
	case $(uname -m) in
		riscv64) ;;
		*) echo "native mode requires a linux/riscv64 host" >&2; exit 1 ;;
	esac

	"$go_cmd" test ./... -count=1 | tee "$out/go-test.log"
	"$go_cmd" test -tags wago_guardpage ./... -count=1 | tee "$out/go-test-guard.log"
	"$go_cmd" vet ./src/core/compiler/backend/railshot/riscv64 ./src/core/compiler/wasm ./src/core/runtime ./src/wago
	"$go_cmd" test ./src/core/runtime -run 'RVV|HWCAP|Misaligned' -count=1 -v \
		| tee "$out/native-capabilities.log"

	"$go_cmd" test ./src/core/runtime ./src/core/runtime/riscv64spike ./src/wago \
		-count="$stress_count" | tee "$out/stress-explicit.log"
	"$go_cmd" test -tags wago_guardpage ./src/core/runtime ./src/wago \
		-run 'Guard|Signal|MemoryGrow|Host|Cancellation|SIMD|V128' -count="$stress_count" \
		| tee "$out/stress-guard.log"

	if [ -n "${WAGO_SPECTEST_DIR:-}" ]; then
		for tags in '' wago_guardpage; do
			label=explicit
			[ -z "$tags" ] || label=guard
			for version in simd relaxed-simd; do
				(
					cd "$root/src/wago"
					if [ -n "$tags" ]; then
						WAGO_SPEC_VERSION="$version" "$go_cmd" test -tags "$tags" \
							-run '^TestSpecSuiteExec$' -count=1 -v
					else
						WAGO_SPEC_VERSION="$version" "$go_cmd" test \
							-run '^TestSpecSuiteExec$' -count=1 -v
					fi
				) >"$out/spec-native-$label-$version.log" 2>&1
				grep 'TOTAL\|^PASS$' "$out/spec-native-$label-$version.log" | tail -2
			done
		done
	fi

	if [ -d "$root/bench" ]; then
		(
			cd "$root/bench"
			"$go_cmd" test -run '^$' -bench "$bench_regex" -wago.bench.isa \
				-benchmem -benchtime="$bench_time" -count="$bench_count"
		) | tee "$out/bench-explicit.txt"
		(
			cd "$root/bench"
			WAGO_BOUNDS=signals "$go_cmd" test -tags wago_guardpage -run '^$' \
				-bench "$bench_regex" -wago.bench.isa -benchmem \
				-benchtime="$bench_time" -count="$bench_count"
		) | tee "$out/bench-guard.txt"
	fi
}

record_host
case $mode in
	qemu) run_qemu ;;
	native) run_native ;;
	*) echo "usage: $0 [qemu|native]" >&2; exit 2 ;;
esac

echo "riscv64 qualification artifacts: $out"
