#!/bin/sh
set -eu
export WAGO_SHARED_SCALAR=0 WAGO_ARM64_EXPERIMENT_INTERVAL_CALL_REGIONS=1
export WAGO_PARITY_CACHE=/Users/work/Code/Web/wasm.fyi/.wasmbench/corpus-cache
go test -tags wago_codegenstats,wago_regalloccheck,wago_guardpage ./src/core/compiler/backend/railshot/arm64 ./src/core/compiler/optimization > experiments/arm64-parity/interval-call-floor-tests.txt 2>&1
go test -tags wago_guardpage -c -o /tmp/parity-interval-call-floor.test ./experiments/arm64-parity
index=0
for variant in old new new old; do
 binary=/tmp/parity-interval-call-packed.test
 if [ "$variant" = new ];then binary=/tmp/parity-interval-call-floor.test;fi
 for id in wago/fastfloat/decimal-parse wago/kissfft/complex-roundtrip; do
  export WAGO_PARITY_CORE_ID="$id"
  name=$(printf '%s' "$id" | tr / _)
  "$binary" -test.run=^$ -test.bench='BenchmarkCachedContract/(Compile|Exec)$' -test.benchtime=500ms -test.count=2 > "experiments/arm64-parity/interval-call-floor-$name-$index-$variant.txt" 2>&1
 done
 index=$((index+1))
done
go build -tags wago_guardpage -o /tmp/paired-interval-call-floor ./experiments/arm64-parity/paired
for phase in compile exec; do
 mode=-core-compile
 if [ "$phase" = exec ];then mode=-core-exec;fi
 WAGO_ARM64_EXPERIMENT_INTERVAL_CALL_REGIONS=0 /tmp/paired-interval-call-floor "$mode" -corpus "$WAGO_PARITY_CACHE" -option interval-call-regions -workloads decimal-parse,complex-roundtrip -phase "$phase" -rounds 10 -budget 250ms > "experiments/arm64-parity/interval-call-floor-paired-$phase.jsonl" 2>&1
done
