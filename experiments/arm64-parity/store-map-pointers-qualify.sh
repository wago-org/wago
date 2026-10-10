#!/bin/sh
set -eu
export WAGO_SHARED_SCALAR=0 WAGO_ARM64_EXPERIMENT_STORE_MAP=1
export WAGO_PARITY_CORPUS=/Users/work/Code/Web/wasm.fyi/corpora/applications
go test -tags wago_codegenstats,wago_regalloccheck,wago_guardpage ./src/core/compiler/backend/railshot/arm64 -run TestStoreMap -count=1 > experiments/arm64-parity/store-map-pointers-tests.txt 2>&1
go test -tags wago_guardpage -c -o /tmp/parity-store-map-pointers.test ./experiments/arm64-parity
index=0
for variant in before after after before; do
 binary=/tmp/parity-store-map-walk.test
 if [ "$variant" = after ]; then binary=/tmp/parity-store-map-pointers.test; fi
 "$binary" -test.run=^$ -test.bench='BenchmarkParity/(ml-inference|vision-components|image-resize|db-sort-merge-join)/(Compile|Exec)$' -test.benchtime=200ms -test.count=2 > "experiments/arm64-parity/store-map-pointers-$index-$variant.txt" 2>&1
 index=$((index+1))
done
