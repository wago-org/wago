#!/bin/sh
set -eu
export WAGO_SHARED_SCALAR=0 WAGO_PARITY_CACHE=/Users/work/Code/Web/wasm.fyi/.wasmbench/corpus-cache
export WAGO_ARM64_EXPERIMENT_SCOPED_LOOP_CONST=0
go test -tags wago_guardpage -c -o /tmp/parity-scoped-loop-contracts.test ./experiments/arm64-parity
for id in wago/coremark/2k-performance wago/json-as-simd/serializeN wago/qoi/encode; do
 export WAGO_PARITY_CORE_ID="$id"
 name=$(printf '%s' "$id" | tr / _)
 index=0
 for variant in off on on off; do
  enabled=0
  if [ "$variant" = on ]; then enabled=1; fi
  WAGO_ARM64_EXPERIMENT_SCOPED_LOOP_CONST="$enabled" /tmp/parity-scoped-loop-contracts.test -test.run=^$ -test.bench='BenchmarkCachedContract/(Compile|Exec)$' -test.benchtime=400ms -test.count=2 > "experiments/arm64-parity/scoped-loop-const-contract-$name-$index-$variant.txt" 2>&1
  index=$((index+1))
 done
done
