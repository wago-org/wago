#!/bin/sh
set -eu

if [ "$#" -ne 2 ]; then
	echo "usage: $0 OUTPUT_DIRECTORY OLD_DEFAULT_OVERRIDES" >&2
	exit 2
fi

output=$1
old_overrides=$2
bench_pattern=${MATRIX_BENCH_PATTERN:-^BenchmarkMatrix(CompileFull|Exec)$}
benchtime=${MATRIX_BENCHTIME:-100ms}
mkdir -p "$output/raw"

run_one() {
	profile=$1
	rep=$2
	overrides=$3
	file="$output/raw/${profile}_r${rep}.txt"
	set -- ./optimization-matrix.test
	if [ -n "${MATRIX_CPU:-}" ]; then
		set -- taskset -c "$MATRIX_CPU" "$@"
	fi
	WAGO_MATRIX_OVERRIDES="$overrides" GOMAXPROCS=1 \
		perl -e 'alarm shift; exec @ARGV' 300 "$@" \
		-test.run '^$' \
		-test.bench "$bench_pattern" \
		-test.benchmem \
		-test.benchtime="$benchtime" \
		-test.count=1 >"$file" 2>&1
}

{
	date -u '+utc=%Y-%m-%dT%H:%M:%SZ'
	printf 'commit='
	if [ -n "${WAGO_MATRIX_COMMIT:-}" ]; then
		printf '%s\n' "$WAGO_MATRIX_COMMIT"
	else
		git -C .. rev-parse HEAD
	fi
	printf 'go_version='
	go version
	printf 'uname='
	uname -a
	printf 'gomaxprocs=1\nbenchtime=%s\nsamples_per_profile=4\n' "$benchtime"
	printf 'bench_pattern=%s\n' "$bench_pattern"
	printf 'old_default_overrides=%s\n' "$old_overrides"
} >"$output/metadata.txt"

# ABBA-balanced order.
run_one old-default 1 "$old_overrides"
run_one new-default 1 ""
run_one new-default 2 ""
run_one old-default 2 "$old_overrides"
run_one new-default 3 ""
run_one old-default 3 "$old_overrides"
run_one old-default 4 "$old_overrides"
run_one new-default 4 ""
