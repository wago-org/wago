#!/bin/sh
set -eu
export WAGO_SHARED_SCALAR=0 WAGO_PARITY_CACHE=/Users/work/Code/Web/wasm.fyi/.wasmbench/corpus-cache
export WAGO_PARITY_CORPUS=/Users/work/Code/Web/wasm.fyi/corpora/applications
index=0
for variant in off on on off; do
 enabled=0
 if [ "$variant" = on ]; then enabled=1; fi
 WAGO_ARM64_EXPERIMENT_SCOPED_LOOP_CONST="$enabled" /tmp/parity-scoped-loop-const.test -test.run=^$ -test.bench='BenchmarkParity/(stats-gini|vision-otsu)/(Compile|Exec)$' -test.benchtime=600ms -test.count=3 > "experiments/arm64-parity/scoped-loop-const-repeat-app-$index-$variant.txt" 2>&1
 WAGO_PARITY_CORE_ID=wago/polybench-gemm/polybench_run WAGO_ARM64_EXPERIMENT_SCOPED_LOOP_CONST="$enabled" /tmp/parity-scoped-loop-const.test -test.run=^$ -test.bench='BenchmarkCachedSimple/(Compile|Exec)$' -test.benchtime=600ms -test.count=3 > "experiments/arm64-parity/scoped-loop-const-repeat-gemm-$index-$variant.txt" 2>&1
 index=$((index+1))
done
