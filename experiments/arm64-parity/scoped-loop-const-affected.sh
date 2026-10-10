#!/bin/sh
set -eu
export WAGO_SHARED_SCALAR=0 WAGO_PARITY_CACHE=/Users/work/Code/Web/wasm.fyi/.wasmbench/corpus-cache
export WAGO_PARITY_CORPUS=/Users/work/Code/Web/wasm.fyi/corpora/applications
binary=/tmp/parity-scoped-loop-const.test
for id in wago/kissfft/complex-roundtrip wago/polybench-adi/polybench_run wago/polybench-correlation/polybench_run wago/polybench-floyd-warshall/polybench_run wago/polybench-gemm/polybench_run wago/polybench-seidel-2d/polybench_run wago/polybench-trisolv/polybench_run wago/utf8proc/normalize-casefold; do
 export WAGO_PARITY_CORE_ID="$id"
 name=$(printf '%s' "$id" | tr / _)
 index=0
 for variant in on off off on; do
  enabled=0
  if [ "$variant" = on ]; then enabled=1; fi
  WAGO_ARM64_EXPERIMENT_SCOPED_LOOP_CONST="$enabled" "$binary" -test.run=^$ -test.bench='BenchmarkCachedSimple/(Compile|Exec)$' -test.benchtime=300ms -test.count=2 > "experiments/arm64-parity/scoped-loop-const-affected-$name-$index-$variant.txt" 2>&1
  index=$((index+1))
 done
done
index=0
for variant in on off off on; do
 enabled=0
 if [ "$variant" = on ]; then enabled=1; fi
 WAGO_ARM64_EXPERIMENT_SCOPED_LOOP_CONST="$enabled" "$binary" -test.run=^$ -test.bench='BenchmarkParity/(stats-gini|vision-otsu)/(Compile|Exec)$' -test.benchtime=300ms -test.count=2 > "experiments/arm64-parity/scoped-loop-const-affected-app-$index-$variant.txt" 2>&1
 index=$((index+1))
done
