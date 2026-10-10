#!/bin/sh
set -eu
export WAGO_SHARED_SCALAR=0 WAGO_ARM64_EXPERIMENT_INTERVAL_CALL_REGIONS=1
export WAGO_PARITY_CACHE=/Users/work/Code/Web/wasm.fyi/.wasmbench/corpus-cache WAGO_PARITY_CORPUS=/Users/work/Code/Web/wasm.fyi/corpora/applications
sh experiments/arm64-parity/interval-call-next-use-qualify.sh
index=0
for variant in baseline weighted next next weighted baseline; do
 binary=/tmp/parity-interval-call-regions.test
 enabled=1
 if [ "$variant" = baseline ]; then enabled=0; fi
 if [ "$variant" = next ]; then binary=/tmp/parity-interval-call-next-use.test; fi
 for id in wago/fastfloat/decimal-parse wago/kissfft/complex-roundtrip; do
  export WAGO_PARITY_CORE_ID="$id"
  name=$(printf '%s' "$id" | tr / _)
  WAGO_ARM64_EXPERIMENT_INTERVAL_CALL_REGIONS="$enabled" "$binary" -test.run=^$ -test.bench='BenchmarkCachedContract/(Compile|Exec)$' -test.benchtime=500ms -test.count=2 > "experiments/arm64-parity/interval-call-next-use-$name-$index-$variant.txt" 2>&1
 done
 index=$((index+1))
done
go build -tags wago_guardpage -o /tmp/paired-interval-call-next-use ./experiments/arm64-parity/paired
WAGO_ARM64_EXPERIMENT_INTERVAL_CALL_REGIONS=0 /tmp/paired-interval-call-next-use -core-compile -corpus "$WAGO_PARITY_CACHE" -option interval-call-regions -workloads decimal-parse,complex-roundtrip -phase compile -rounds 10 -budget 250ms > experiments/arm64-parity/interval-call-next-use-compile-pairs.jsonl 2>&1
