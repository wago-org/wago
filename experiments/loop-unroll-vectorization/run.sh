#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/../.."
out=${WAGO_EXPERIMENT_RESULTS:-experiments/loop-unroll-vectorization/results/final}
for key in ${!WAGO_@}; do unset "$key"; done
if [[ -d "$out" ]] && compgen -G "$out/*.txt" >/dev/null; then
  echo "Choose an empty results directory. Existing samples are preserved." >&2
  exit 1
fi
mkdir -p "$out"
go test -c -o "$out/experiment.test" ./src/core/compiler/backend/railshot/amd64
# Compilation of this test executable is outside all timing samples.
order() {
  if (( round % 2 == 0 )); then
    for (( i=$#; i>0; i-- )); do printf '%s\n' "${!i}"; done
  else
    printf '%s\n' "$@"
  fi
}
for round in 1 2 3 4 5 6; do
  printf 'round=%d\n' "$round"
  for variant in $(order A B C D E F G H); do
    WAGO_LOOP_SUM_EXPERIMENT="$variant" GOMAXPROCS=1 "$out/experiment.test" -test.run '^$' -test.bench '^(BenchmarkLinearSumNoWrapAMD64|BenchmarkCompileLinearSumAMD64)$' -test.benchmem -test.benchtime 100ms >> "$out/A-final-$variant.txt"
    WAGO_LOOP_SUM_EXPERIMENT="$variant" GOMAXPROCS=1 "$out/experiment.test" -test.run '^$' -test.bench '^BenchmarkExperimentalSumAddress$' -test.benchmem -test.benchtime 100ms >> "$out/A-address-$variant.txt"
  done
  for mode in $(order scalar count2 count4 guard2 simd2 simd4 vector-i32 vector-f32); do
    [[ "$mode" != scalar ]] || mode=''
    WAGO_LOOP_REPLICATION="$mode" GOMAXPROCS=1 "$out/experiment.test" -test.run '^$' -test.bench '^BenchmarkExperimentalReplication$/[^/]+/compile$' -test.benchmem -test.benchtime 100ms >> "$out/CDE-${mode:-scalar}.txt"
    WAGO_LOOP_REPLICATION="$mode" GOMAXPROCS=1 "$out/experiment.test" -test.run '^$' -test.bench '^BenchmarkExperimentalReplication$/(map-i32|dependent-i32|dependent-f64|pointer|simd-i32|map-f32)/execute$/(0|1|2|3|4|5|6|7|8|9|15|16|17|511|512|513|8192|262144|1048576|2097152|4194304)$' -test.benchmem -test.benchtime 50ms >> "$out/CDE-${mode:-scalar}.txt"
  done
  for mode in $(order scalar count2 count4 vector-f32 vector-i32); do
    [[ "$mode" != scalar ]] || mode=''
    WAGO_LOOP_REPLICATION="$mode" GOMAXPROCS=1 "$out/experiment.test" -test.run '^$' -test.bench '^BenchmarkExperimentalRejectedCompile$' -test.benchmem -test.benchtime 100ms >> "$out/rejected-${mode:-scalar}.txt"
  done
  for on in $(order 0 1); do
    WAGO_LOOP_REDUCTION_FORMS="$on" GOMAXPROCS=1 "$out/experiment.test" -test.run '^$' -test.bench '^BenchmarkExperimentalReduction$/[^/]+/compile$' -test.benchmem -test.benchtime 100ms >> "$out/B-$on.txt"
    WAGO_LOOP_REDUCTION_FORMS="$on" GOMAXPROCS=1 "$out/experiment.test" -test.run '^$' -test.bench '^BenchmarkExperimentalReduction$/[^/]+/execute$/(0|1|2|3|4|5|6|7|8|9|15|16|17|511|512|513|8192)$' -test.benchmem -test.benchtime 100ms >> "$out/B-$on.txt"
  done
  for mode in $(order scalar pair128 wide256 adjacent128 adjacent256); do
    WAGO_LOOP_F64_MODE="$mode" GOMAXPROCS=1 "$out/experiment.test" -test.run '^$' -test.bench '^BenchmarkExperimentalExistingF64$' -test.benchmem -test.benchtime 100ms >> "$out/f64-$mode.txt"
  done
  for mode in $(order scalar count2 vector-f32 vector-i32); do
    [[ "$mode" != scalar ]] || mode=''
    WAGO_LOOP_REPLICATION="$mode" GOMAXPROCS=1 "$out/experiment.test" -test.run '^$' -test.bench '^BenchmarkExperimentalCorpusCompile$' -test.benchmem -test.benchtime 100ms >> "$out/corpus-${mode:-scalar}.txt"
  done
  GOMAXPROCS=1 experiments/loop-unroll-vectorization/results/baseline.test -test.run '^$' -test.bench '^(BenchmarkLinearSumNoWrapAMD64|BenchmarkCompileLinearSumAMD64)$' -test.benchmem -test.benchtime 100ms >> "$out/default-baseline.txt"
  GOMAXPROCS=1 experiments/loop-unroll-vectorization/results/baseline.test -test.run '^$' -test.bench '^(BenchmarkExperimentalRejectedCompile|BenchmarkExperimentalCorpusCompile)$' -test.benchmem -test.benchtime 100ms >> "$out/default-baseline-extra.txt"
  GOMAXPROCS=1 "$out/experiment.test" -test.run '^$' -test.bench '^(BenchmarkLinearSumNoWrapAMD64|BenchmarkCompileLinearSumAMD64)$' -test.benchmem -test.benchtime 100ms >> "$out/default-final.txt"
 done
