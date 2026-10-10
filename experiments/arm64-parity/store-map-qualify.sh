#!/bin/sh
set -eu
export WAGO_SHARED_SCALAR=0 WAGO_ARM64_EXPERIMENT_STORE_MAP=1
export WAGO_PARITY_CORPUS=/Users/work/Code/Web/wasm.fyi/corpora/applications
go test -tags wago_codegenstats,wago_regalloccheck,wago_guardpage ./src/core/compiler/backend/railshot ./src/core/compiler/backend/railshot/arm64 ./src/core/runtime ./src/core/encoder/arm64 ./src/core/compiler/optimization > experiments/arm64-parity/store-map-integrated-tests.txt 2>&1
go test -tags wago_guardpage -c -o /tmp/parity-store-map.test ./experiments/arm64-parity
WAGO_PARITY_CODE_DIR=/tmp/store-map-screen /tmp/parity-store-map.test -test.run=^$ -test.bench='BenchmarkParity/.*/Exec$' -test.benchtime=1x > experiments/arm64-parity/store-map-screen-oracles.txt 2>&1
index=0
for mode in off on on off; do
 flag=0
 if [ "$mode" = on ]; then flag=1; fi
 WAGO_ARM64_EXPERIMENT_STORE_MAP=$flag /tmp/parity-store-map.test -test.run=^$ -test.bench='BenchmarkParity/(vision-components|compiler-register-allocation|ml-knn|video-dct)/(Compile|Exec)$' -test.benchtime=100ms -test.count=2 > "experiments/arm64-parity/store-map-first-$index-$mode.txt" 2>&1
 index=$((index+1))
done
