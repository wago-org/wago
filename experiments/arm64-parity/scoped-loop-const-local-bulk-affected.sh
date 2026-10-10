#!/bin/sh
set -eu
export WAGO_SHARED_SCALAR=0 WAGO_ARM64_EXPERIMENT_SCOPED_LOOP_CONST=1
export WAGO_PARITY_CACHE=/Users/work/Code/Web/wasm.fyi/.wasmbench/corpus-cache
for id in wago/coremark/2k-performance wago/json-as-simd/serializeN wago/monocypher/aead-blake2b wago/polybench-cholesky/polybench_run wago/polybench-gramschmidt/polybench_run wago/polybench-lu/polybench_run wago/polybench-nussinov/polybench_run wago/sha256/hashN wago/utf-as-simd/validateN wago/utf8proc/normalize-casefold; do
 export WAGO_PARITY_CORE_ID="$id"
 name=$(printf '%s' "$id" | tr / _)
 index=0
 for variant in old new new old; do
  enabled=0
  if [ "$variant" = new ]; then enabled=1; fi
  WAGO_ARM64_EXPERIMENT_SCOPED_LOCAL_BULK_PROOF="$enabled" /tmp/parity-scoped-loop-local-bulk.test -test.run=^$ -test.bench='BenchmarkCachedContract/(Compile|Exec)$' -test.benchtime=400ms -test.count=2 > "experiments/arm64-parity/scoped-loop-const-local-bulk-affected-$name-$index-$variant.txt" 2>&1
  index=$((index+1))
 done
done
