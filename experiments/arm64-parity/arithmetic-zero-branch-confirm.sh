#!/bin/sh
set -eu
export WAGO_SHARED_SCALAR=0 WAGO_PARITY_CACHE=/Users/work/Code/Web/wasm.fyi/.wasmbench/corpus-cache
index=0
for state in 0 1 1 0;do
 for id in wago/coremark/2k-performance wago/pcre2/compile-match wago/polybench-adi/polybench_run wago/polybench-cholesky/polybench_run wago/polybench-correlation/polybench_run wago/polybench-floyd-warshall/polybench_run;do
  name=$(printf '%s' "$id" | tr / _)
  WAGO_PARITY_CORE_ID="$id" WAGO_ARM64_EXPERIMENT_ARITHMETIC_ZERO="$state" /tmp/parity-arithmetic-zero-branch.test -test.run=^$ -test.bench='BenchmarkCachedContract/(Compile|Exec)$' -test.benchtime=300ms -test.count=2 > "experiments/arm64-parity/arithmetic-zero-branch-confirm-$name-$index-$state.txt" 2>&1
 done
 index=$((index+1))
done
