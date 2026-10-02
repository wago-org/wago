#!/bin/sh
set -eu

if [ "$#" -ne 1 ]; then
	echo "usage: $0 OUTPUT_DIRECTORY" >&2
	exit 2
fi

output=$1
mkdir -p "$output/raw"

run_binary() {
	if [ -n "${MATRIX_CPU:-}" ]; then
		taskset -c "$MATRIX_CPU" ./optimization-matrix.test "$@"
	else
		./optimization-matrix.test "$@"
	fi
}

run_binary -test.run '^TestOptimizationMatrixInventory$' -test.v >"$output/inventory.txt"

{
	date -u '+utc=%Y-%m-%dT%H:%M:%SZ'
	printf 'commit='
	git -C .. rev-parse HEAD
	printf 'go_version='
	go version
	printf 'uname='
	uname -a
	printf 'gomaxprocs=1\n'
	printf 'benchtime=100ms\n'
	printf 'samples_per_state=4\n'
	printf 'cpu_affinity=%s\n' "${MATRIX_CPU:-none}"
} >"$output/metadata.txt"

catalog_opts=$(awk -F '\t' '$1 == "MATRIX_OPT" {print $2}' "$output/inventory.txt")
opts=${MATRIX_OPTS:-$catalog_opts}
for opt in $opts; do
	if ! printf '%s\n' "$catalog_opts" | grep -qx "$opt"; then
		echo "unknown MATRIX_OPTS entry: $opt" >&2
		exit 2
	fi
done
count=$(printf '%s\n' $opts | awk 'NF {n++} END {print n+0}')
index=0

run_one() {
	opt=$1
	state=$2
	rep=$3
	index=$((index + 1))
	file="$output/raw/${opt}_${state}_r${rep}.txt"
	echo "MATRIX_PROGRESS $index/$((count * 8)) $opt=$state repeat=$rep"
	set +e
	set -- ./optimization-matrix.test
	if [ -n "${MATRIX_CPU:-}" ]; then
		set -- taskset -c "$MATRIX_CPU" "$@"
	fi
	WAGO_MATRIX_OPT="$opt" WAGO_MATRIX_ON="$state" GOMAXPROCS=1 \
		perl -e 'alarm shift; exec @ARGV' 300 "$@" \
		-test.run '^$' \
		-test.bench '^BenchmarkMatrix(CompileFull|Exec)$' \
		-test.benchmem \
		-test.benchtime=100ms \
		-test.count=1 >"$file" 2>&1
	status=$?
	set -e
	printf '%s\n' "$status" >"$file.exit"
}

# One untimed process warms the binary, module pages, and compiler caches.
WAGO_MATRIX_OPT=$(printf '%s\n' $opts | sed -n '1p') WAGO_MATRIX_ON=true GOMAXPROCS=1 \
	run_binary -test.run '^$' \
	-test.bench '^BenchmarkMatrix(CompileFull|Exec)/(tiny|fib_iter)' \
	-test.benchmem -test.benchtime=25ms -test.count=1 >"$output/warmup.txt" 2>&1

for opt in $opts; do
	run_one "$opt" true 1
	run_one "$opt" false 1
	run_one "$opt" false 2
	run_one "$opt" true 2
	run_one "$opt" false 3
	run_one "$opt" true 3
	run_one "$opt" true 4
	run_one "$opt" false 4
done

echo "MATRIX_DONE $count optimizations"
