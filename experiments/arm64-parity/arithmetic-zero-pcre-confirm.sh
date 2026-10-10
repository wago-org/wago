#!/bin/sh
set -eu
export WAGO_SHARED_SCALAR=0 WAGO_PARITY_CACHE=/Users/work/Code/Web/wasm.fyi/.wasmbench/corpus-cache WAGO_PARITY_CORE_ID=wago/pcre2/compile-match
index=0
for state in 1 0 1 0 0 1;do
 WAGO_ARM64_EXPERIMENT_ARITHMETIC_ZERO="$state" /tmp/parity-arithmetic-zero.test -test.run=^$ -test.bench='BenchmarkCachedContract/Exec$' -test.benchtime=500ms -test.count=2 > "experiments/arm64-parity/arithmetic-zero-pcre-repeat-$index-$state.txt" 2>&1
 index=$((index+1))
done
