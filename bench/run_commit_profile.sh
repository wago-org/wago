#!/bin/sh
set -eu

if [ "$#" -ne 3 ]; then
	echo "usage: $0 OUTPUT_DIRECTORY OLD_BINARY NEW_BINARY" >&2
	exit 2
fi

output=$1
old_binary=$2
new_binary=$3
bench_pattern=${MATRIX_BENCH_PATTERN:-^BenchmarkMatrix(CompileFull|Exec)$}
benchtime=${MATRIX_BENCHTIME:-100ms}
mkdir -p "$output/raw"

run_one() {
	profile=$1
	rep=$2
	binary=$3
	file="$output/raw/${profile}_r${rep}.txt"
	set -- "$binary"
	if [ -n "${MATRIX_CPU:-}" ]; then
		set -- taskset -c "$MATRIX_CPU" "$@"
	fi
	GOMAXPROCS=1 perl -e 'alarm shift; exec @ARGV' 300 "$@" \
		-test.run '^$' \
		-test.bench "$bench_pattern" \
		-test.benchmem \
		-test.benchtime="$benchtime" \
		-test.count=1 >"$file" 2>&1
}

run_one old-default 1 "$old_binary"
run_one new-default 1 "$new_binary"
run_one new-default 2 "$new_binary"
run_one old-default 2 "$old_binary"
run_one new-default 3 "$new_binary"
run_one old-default 3 "$old_binary"
run_one old-default 4 "$old_binary"
run_one new-default 4 "$new_binary"
