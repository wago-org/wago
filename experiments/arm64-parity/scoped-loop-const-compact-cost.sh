#!/bin/sh
set -eu
test -x /tmp/parity-scoped-loop-policy.test
test -s experiments/arm64-parity/scoped-loop-const-paired-exec.jsonl
export WAGO_SHARED_SCALAR=0 WAGO_ARM64_EXPERIMENT_SCOPED_LOOP_CONST=0 WAGO_ARM64_EXPERIMENT_SMALL_MUL_SHIFT=0
export WAGO_PARITY_CACHE=/Users/work/Code/Web/wasm.fyi/.wasmbench/corpus-cache WAGO_PARITY_CORPUS=/Users/work/Code/Web/wasm.fyi/corpora/applications
index=0
for variant in baseline old new new old baseline; do
 binary=/tmp/parity-constant-shift-rollback.test
 if [ "$variant" = old ];then binary=/tmp/parity-small-mul-shift.test;fi
 if [ "$variant" = new ];then binary=/tmp/parity-scoped-loop-policy.test;fi
 "$binary" -test.run=^$ -test.bench='BenchmarkParity/(stats-gini|vision-otsu)/Compile$' -test.benchtime=500ms -test.count=2 > "experiments/arm64-parity/scoped-loop-const-compact-cost-app-$index-$variant.txt" 2>&1
 for id in wago/fastfloat/decimal-parse wago/pcre2/compile-match; do
  export WAGO_PARITY_CORE_ID="$id"
  name=$(printf '%s' "$id" | tr / _)
  "$binary" -test.run=^$ -test.bench='BenchmarkCachedSimple/Compile$' -test.benchtime=500ms -test.count=2 > "experiments/arm64-parity/scoped-loop-const-compact-cost-$name-$index-$variant.txt" 2>&1
 done
 index=$((index+1))
done
