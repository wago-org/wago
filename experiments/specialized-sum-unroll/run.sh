#!/usr/bin/env bash
# Build all inputs before timing. Run from any directory within the repository.
set -euo pipefail
root_dir="$(git rev-parse --show-toplevel)"
cd "$root_dir"
exp_dir="$root_dir/experiments/specialized-sum-unroll"
result_dir="${1:?usage: run.sh NEW_RESULT_DIRECTORY [CPU]}"
result_dir="$(realpath -m "$result_dir")"
cpu="${2:-2}"
if [[ -e "$result_dir" ]]; then
    echo 'Use a new result directory.' >&2
    exit 1
fi
mkdir -p "$result_dir"
build_dir="$(mktemp -d /tmp/wago-sum-build.XXXXXX)"
trap 'rm -rf "$build_dir"' EXIT
export GOFLAGS=-buildvcs=false
export GOMAXPROCS=1
mkdir -p "$build_dir/base"
git archive "$(cat "$exp_dir/tests-commit.txt")" | tar -x -C "$build_dir/base"
# Copy only the later threshold benchmark; baseline compiler sources stay fixed.
cp src/core/compiler/backend/railshot/amd64/sum_unroll_threshold_test.go "$build_dir/base/src/core/compiler/backend/railshot/amd64/"
(cd "$build_dir/base" && go test -c -o "$build_dir/baseline.test" ./src/core/compiler/backend/railshot/amd64 && go test -c -o "$build_dir/corpus-baseline.test" ./bench/suite)
go test -c -o "$build_dir/normal.test" ./src/core/compiler/backend/railshot/amd64
go test -c -tags=wago_sumunroll -o "$build_dir/experiment.test" ./src/core/compiler/backend/railshot/amd64
go test -c -o "$build_dir/corpus-normal.test" ./bench/suite
sha256sum "$build_dir"/*.test > "$result_dir/binaries-sha256.txt"
for variant in baseline A B C D E; do
    WAGO_SUM_VARIANT="$variant" go test -tags=wago_sumunroll,wago_codegenstats,wago_regalloccheck ./src/core/compiler/backend/railshot/amd64 -run 'TestSumUnroll|TestLinearSum' -count=1 -v > "$result_dir/$variant-tests.txt" 2>&1
    WAGO_SUM_VARIANT="$variant" WAGO_SUM_ARTIFACT_DIR="$result_dir/native-$variant" go test -tags=wago_sumunroll,wago_codegenstats,wago_regalloccheck ./src/core/compiler/backend/railshot/amd64 -run '^TestSumUnrollArtifacts$' -count=1
    for pressure in 0 4 12; do
        objdump -D -b binary -m i386:x86-64 "$result_dir/native-$variant/pressure$pressure.bin" > "$result_dir/native-$variant/pressure$pressure.asm"
    done
done
sample=(python3 "$exp_dir/sample.py" --cpu "$cpu" --baseline "$build_dir/baseline.test" --candidate "$build_dir/experiment.test")
"${sample[@]}" --variants A B --bench '^BenchmarkSumUnroll(Execute|Compile)$' --out "$result_dir/run1-AB"
"${sample[@]}" --variants C D E --bench '^BenchmarkSumUnroll(Execute|Compile)$' --out "$result_dir/run1-CDE"
# A distinct run repeats gains and retained adjacent-workload regressions.
"${sample[@]}" --variants A B D E --bench '^BenchmarkSumUnroll(Execute|Compile)$' --out "$result_dir/run2"
"${sample[@]}" --variants A D --bench '^BenchmarkSumUnrollThreshold$' --out "$result_dir/threshold1"
"${sample[@]}" --variants A D --bench '^BenchmarkSumUnrollThreshold$' --out "$result_dir/threshold2"
# Hold the factor fixed when comparing accumulator counts.
for factor in A D; do
    candidate=B
    if [[ "$factor" == D ]]; then candidate=E; fi
    python3 "$exp_dir/sample.py" --cpu "$cpu" --baseline "$build_dir/experiment.test" --baseline-variant "$factor" --candidate "$build_dir/experiment.test" --variants "$candidate" --bench '^BenchmarkSumUnrollExecute$/addr[01]$/n(8192|262144)$' --out "$result_dir/chains-$factor"
done
python3 "$exp_dir/sample.py" --cpu "$cpu" --baseline "$build_dir/baseline.test" --candidate "$build_dir/normal.test" --variants default --bench '^BenchmarkSumUnroll(Execute|Compile)$' --out "$result_dir/default-sum"
python3 "$exp_dir/sample.py" --cpu "$cpu" --baseline "$build_dir/corpus-baseline.test" --candidate "$build_dir/corpus-normal.test" --variants default --cwd "$root_dir/bench/suite" --corpus all --bench '^(BenchmarkCompile|BenchmarkExec)$/(memory|many_funcs|linked_list|blake-as-simd|utf-as-simd)(\.|$)' --out "$result_dir/default-corpus"
python3 "$exp_dir/analyze.py" "$result_dir/run1-AB" "$result_dir/run1-CDE" "$result_dir/run2" "$result_dir/chains-A" "$result_dir/chains-D" "$result_dir/default-sum" "$result_dir/default-corpus" "$result_dir/threshold1" "$result_dir/threshold2"
