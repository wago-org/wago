#!/usr/bin/env bash
# Reproduce the bounded mitigation study. Optional third argument is a frozen
# normal corpus binary from before this study, for the disabled control.
set -euo pipefail
root_dir="$(git rev-parse --show-toplevel)"
cd "$root_dir"
exp_dir="$root_dir/experiments/specialized-sum-unroll"
result_dir="$(realpath -m "${1:?usage: mitigation.sh NEW_RESULT_DIRECTORY [CPU] [PREVIOUS_NORMAL_CORPUS_BINARY]}")"
cpu="${2:-2}"
previous="${3:-}"
if [[ -n "$previous" ]]; then previous="$(realpath "$previous")"; fi
if [[ -e "$result_dir" ]]; then echo 'Use a new result directory.' >&2; exit 1; fi
mkdir -p "$result_dir"
build_dir="$(mktemp -d /tmp/wago-sum-mitigation.XXXXXX)"
trap 'rm -rf "$build_dir"' EXIT
export GOFLAGS=-buildvcs=false GOMAXPROCS=1
go test -c -o "$build_dir/normal.test" ./src/core/compiler/backend/railshot/amd64
go test -c -tags=wago_sumunroll -o "$build_dir/all.test" ./src/core/compiler/backend/railshot/amd64
go test -c -o "$build_dir/corpus-normal.test" ./bench/suite
go test -c -tags=wago_sumunroll -o "$build_dir/corpus-all.test" ./bench/suite
go test -c -tags=wago_sumunroll,wago_codegenstats,wago_regalloccheck -o "$build_dir/diag.test" ./src/core/compiler/backend/railshot/amd64
go test -c -tags=wago_sumunroll,wago_codegenstats,wago_regalloccheck -o "$build_dir/corpus-diag.test" ./bench/suite
sha256sum "$build_dir"/*.test > "$result_dir/binaries-sha256.txt"
for variant in baseline D P DR PR H T64; do
    (cd src/core/compiler/backend/railshot/amd64 && WAGO_SUM_VARIANT="$variant" "$build_dir/diag.test" -test.run 'TestSumUnroll|TestLinearSum' -test.count 1 -test.v) > "$result_dir/$variant-tests.txt" 2>&1
    if [[ "$variant" == H || "$variant" == T64 ]]; then continue; fi
    WAGO_SUM_VARIANT="$variant" WAGO_SUM_ARTIFACT_DIR="$result_dir/native-$variant" "$build_dir/diag.test" -test.run '^TestSumUnrollArtifacts$' > "$result_dir/$variant-native.txt" 2>&1
    (cd bench/suite && WAGO_SUM_VARIANT="$variant" WAGO_SUM_ARTIFACT_DIR="$result_dir/native-corpus-$variant" "$build_dir/corpus-diag.test" -test.run '^TestSumUnrollCorpusAdmission$' -wago.corpus all) > "$result_dir/$variant-admission.txt" 2>&1
    for pressure in 0 4 12; do objdump -D -b binary -m i386:x86-64 "$result_dir/native-$variant/pressure$pressure.bin" > "$result_dir/native-$variant/pressure$pressure.asm"; done
    objdump -D -b binary -m i386:x86-64 "$result_dir/native-corpus-$variant/memory.bin" > "$result_dir/native-corpus-$variant/memory.asm"
done
(cd src/core/compiler/backend/railshot/amd64 && WAGO_SUM_VARIANT=DR WAGO_SUM_ARTIFACT_DIR="$result_dir/admission" "$build_dir/diag.test" -test.run '^TestSumUnrollHeadroomCorpus$') > "$result_dir/headroom.txt" 2>&1
for variant in DR PR; do
    (cd bench/suite && WAGO_SUM_VARIANT="$variant" "$build_dir/corpus-diag.test" -test.run '^TestSumUnrollMitigationAllocations$' -wago.corpus memory) > "$result_dir/$variant-public-capacity.txt" 2>&1
done
sample=(python3 "$exp_dir/sample.py" --cpu "$cpu" --benchtime 100ms)
corpus=(--cwd "$root_dir/bench/suite" --corpus 'memory,linked_list,blake-as-simd,utf-as-simd,yyjson,xxhash,drwav,seqtk-fastq-to-fasta' --bench '^(BenchmarkCompile|BenchmarkExec|BenchmarkCompileFull|BenchmarkSumUnrollLifecycle)$')
"${sample[@]}" --baseline "$build_dir/corpus-normal.test" --candidate "$build_dir/corpus-all.test" --variants DR "${corpus[@]}" --out "$result_dir/corpus-DR-run1"
"${sample[@]}" --baseline "$build_dir/normal.test" --candidate "$build_dir/all.test" --variants P --bench '^BenchmarkSumUnroll(Followup|Compile)$' --out "$result_dir/kernel-P-run1"
"${sample[@]}" --baseline "$build_dir/all.test" --baseline-variant D --candidate "$build_dir/all.test" --variants P --bench '^BenchmarkSumUnrollFollowup$/addr[01]$/n(0|8|16|17|33|64|128|512|8192|262144|8388607)$' --out "$result_dir/direct-D-P"
"${sample[@]}" --baseline "$build_dir/corpus-all.test" --baseline-variant D --candidate "$build_dir/corpus-all.test" --variants DR "${corpus[@]}" --out "$result_dir/direct-D-DR"
if [[ -n "$previous" ]]; then
    "${sample[@]}" --baseline "$previous" --candidate "$build_dir/corpus-normal.test" --variants baseline "${corpus[@]}" --out "$result_dir/disabled-control"
fi
# Fixed confirmation after initial evidence. PR is not timed: P did not show
# enough incremental benefit to justify a combined candidate.
"${sample[@]}" --baseline "$build_dir/corpus-normal.test" --candidate "$build_dir/corpus-all.test" --variants DR "${corpus[@]}" --out "$result_dir/corpus-DR-repeat"
"${sample[@]}" --baseline "$build_dir/normal.test" --candidate "$build_dir/all.test" --variants P --bench '^BenchmarkSumUnrollFollowup$/addr[01]$/n(0|8|16|17|33|64|128|512|8192|262144|8388607)$' --out "$result_dir/kernel-P-repeat"
"${sample[@]}" --baseline "$build_dir/all.test" --baseline-variant D --candidate "$build_dir/all.test" --variants P --bench '^BenchmarkSumUnrollFollowup$/addr[01]$/n(0|8|16|17|33|64|128|512|8192|262144|8388607)$' --out "$result_dir/direct-D-P-repeat"
"${sample[@]}" --baseline "$build_dir/corpus-all.test" --baseline-variant D --candidate "$build_dir/corpus-all.test" --variants DR --cwd "$root_dir/bench/suite" --corpus memory --bench '^(BenchmarkCompile|BenchmarkExec|BenchmarkCompileFull|BenchmarkSumUnrollLifecycle)$' --out "$result_dir/direct-D-DR-repeat"
"${sample[@]}" --baseline "$build_dir/corpus-normal.test" --candidate "$build_dir/corpus-all.test" --variants P --cwd "$root_dir/bench/suite" --corpus memory --bench '^(BenchmarkCompile|BenchmarkExec|BenchmarkCompileFull|BenchmarkSumUnrollLifecycle)$' --out "$result_dir/corpus-memory-P"
result_sets=("$result_dir/corpus-DR-run1" "$result_dir/kernel-P-run1" "$result_dir/direct-D-P" "$result_dir/direct-D-DR" "$result_dir/corpus-DR-repeat" "$result_dir/kernel-P-repeat" "$result_dir/direct-D-P-repeat" "$result_dir/direct-D-DR-repeat" "$result_dir/corpus-memory-P")
if [[ -n "$previous" ]]; then result_sets+=("$result_dir/disabled-control"); fi
python3 "$exp_dir/analyze.py" "${result_sets[@]}"
