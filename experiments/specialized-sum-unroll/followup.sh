#!/usr/bin/env bash
# Reproduce the fixed follow-up study. Candidate selection/repetition is recorded
# in the report; this script retains all initial discovery measurements.
set -euo pipefail
root_dir="$(git rev-parse --show-toplevel)"
cd "$root_dir"
exp_dir="$root_dir/experiments/specialized-sum-unroll"
result_dir="$(realpath -m "${1:?usage: followup.sh NEW_RESULT_DIRECTORY [CPU]}")"
cpu="${2:-2}"
if [[ -e "$result_dir" ]]; then echo 'Use a new result directory.' >&2; exit 1; fi
mkdir -p "$result_dir"
build_dir="$(mktemp -d /tmp/wago-sum-followup.XXXXXX)"
trap 'rm -rf "$build_dir"' EXIT
export GOFLAGS=-buildvcs=false GOMAXPROCS=1
go test -c -o "$build_dir/normal.test" ./src/core/compiler/backend/railshot/amd64
go test -c -tags=wago_sumunroll -o "$build_dir/all.test" ./src/core/compiler/backend/railshot/amd64
go test -c -o "$build_dir/corpus-normal.test" ./bench/suite
go test -c -tags=wago_sumunroll -o "$build_dir/corpus-all.test" ./bench/suite
sha256sum "$build_dir"/*.test > "$result_dir/binaries-sha256.txt"
for variant in baseline D H T64 T128 T256; do
    WAGO_SUM_VARIANT="$variant" go test -tags=wago_sumunroll,wago_codegenstats,wago_regalloccheck ./src/core/compiler/backend/railshot/amd64 -run 'TestSumUnroll|TestLinearSum' -count=1 -v > "$result_dir/$variant-tests.txt" 2>&1
    WAGO_SUM_VARIANT="$variant" WAGO_SUM_ARTIFACT_DIR="$result_dir/native-$variant" go test -tags=wago_sumunroll,wago_codegenstats,wago_regalloccheck ./src/core/compiler/backend/railshot/amd64 -run '^TestSumUnrollArtifacts$' -count=1
    WAGO_SUM_VARIANT="$variant" WAGO_SUM_ARTIFACT_DIR="$result_dir/native-corpus-$variant" go test -tags=wago_sumunroll,wago_codegenstats,wago_regalloccheck ./bench/suite -run '^TestSumUnrollCorpusAdmission$' -wago.corpus all -count=1 > "$result_dir/$variant-admission.txt" 2>&1
    objdump -D -b binary -m i386:x86-64 "$result_dir/native-corpus-$variant/memory.bin" > "$result_dir/native-corpus-$variant/memory.asm"
    for pressure in 0 4 12; do objdump -D -b binary -m i386:x86-64 "$result_dir/native-$variant/pressure$pressure.bin" > "$result_dir/native-$variant/pressure$pressure.asm"; done
done
sample=(python3 "$exp_dir/sample.py" --cpu "$cpu")
corpus_args=(--baseline "$build_dir/corpus-normal.test" --candidate "$build_dir/corpus-all.test" --cwd "$root_dir/bench/suite" --corpus 'memory,linked_list,blake-as-simd,utf-as-simd,yyjson,xxhash,drwav' --bench '^(BenchmarkCompile|BenchmarkExec|BenchmarkCompileFull|BenchmarkSumUnrollLifecycle)$')
# Real-workload qualification precedes candidate kernel comparisons.
"${sample[@]}" "${corpus_args[@]}" --variants D --out "$result_dir/corpus-D-run1"
"${sample[@]}" --baseline "$build_dir/normal.test" --candidate "$build_dir/all.test" --variants H --out "$result_dir/kernel-H-run1" --bench '^BenchmarkSumUnroll(Followup|Compile)$'
"${sample[@]}" --baseline "$build_dir/normal.test" --candidate "$build_dir/all.test" --variants T64 T128 T256 --out "$result_dir/threshold-discovery" --bench '^BenchmarkSumUnrollFollowup$/addr[01]$/n(0|8|16|64|65|128|129|256|257|512|8192|262144|8388607)$'
"${sample[@]}" --baseline "$build_dir/normal.test" --candidate "$build_dir/all.test" --variants T64 T128 T256 --out "$result_dir/threshold-compile" --bench '^BenchmarkSumUnrollCompile$'
# Frozen confirmation choices. No further threshold or layout tuning.
"${sample[@]}" --baseline "$build_dir/normal.test" --candidate "$build_dir/all.test" --variants H T64 --out "$result_dir/kernel-repeat" --bench '^BenchmarkSumUnroll(Followup|Compile)$'
"${sample[@]}" --baseline "$build_dir/all.test" --baseline-variant D --candidate "$build_dir/all.test" --variants H --out "$result_dir/direct-D-H" --bench '^BenchmarkSumUnrollFollowup$/addr[01]$/n(16|64|128|512|8192|262144|8388607)$'
"${sample[@]}" "${corpus_args[@]}" --variants D --out "$result_dir/corpus-D-repeat"
"${sample[@]}" "${corpus_args[@]}" --variants H T64 --out "$result_dir/corpus-H-T64"
"${sample[@]}" --baseline "$build_dir/corpus-all.test" --candidate "$build_dir/corpus-all.test" --variants D --cwd "$root_dir/bench/suite" --corpus memory,linked_list,blake-as-simd,utf-as-simd,yyjson,xxhash,drwav --bench '^(BenchmarkCompile|BenchmarkExec|BenchmarkCompileFull|BenchmarkSumUnrollLifecycle)$' --out "$result_dir/corpus-same-binary-D"
"${sample[@]}" --baseline "$build_dir/corpus-normal.test" --candidate "$build_dir/corpus-all.test" --variants H T64 --cwd "$root_dir/bench/suite" --corpus memory --bench '^(BenchmarkCompile|BenchmarkExec|BenchmarkCompileFull|BenchmarkSumUnrollLifecycle)$' --out "$result_dir/corpus-memory-repeat"
python3 "$exp_dir/analyze.py" "$result_dir/corpus-D-run1" "$result_dir/kernel-H-run1" "$result_dir/threshold-discovery" "$result_dir/threshold-compile" "$result_dir/kernel-repeat" "$result_dir/direct-D-H" "$result_dir/corpus-D-repeat" "$result_dir/corpus-H-T64" "$result_dir/corpus-same-binary-D" "$result_dir/corpus-memory-repeat"
