#!/bin/sh
set -eu
export WAGO_SHARED_SCALAR=0 WAGO_ARM64_EXPERIMENT_STORE_MAP=1
export WAGO_PARITY_CORPUS=/Users/work/Code/Web/wasm.fyi/corpora/applications
go test -tags wago_codegenstats,wago_regalloccheck,wago_guardpage ./src/core/compiler/backend/railshot ./src/core/compiler/backend/railshot/arm64 ./src/core/runtime ./src/core/encoder/arm64 ./src/core/compiler/optimization > experiments/arm64-parity/store-map-mitigated-tests.txt 2>&1
go test -tags wago_guardpage -c -o /tmp/parity-store-map-mitigated.test ./experiments/arm64-parity
index=0
for mode in on off off on; do
 flag=0
 if [ "$mode" = on ]; then flag=1; fi
 WAGO_ARM64_EXPERIMENT_STORE_MAP=$flag WAGO_PARITY_CODE_DIR=/tmp/store-map-mitigated-$mode /tmp/parity-store-map-mitigated.test -test.run=^$ -test.bench='BenchmarkParity/(vision-components|video-dct|ml-inference|image-composite|image-resize|vision-dilation|stats-quickselect|ml-convolution)/(Compile|Exec)$' -test.benchtime=100ms -test.count=2 > "experiments/arm64-parity/store-map-mitigated-$index-$mode.txt" 2>&1
 index=$((index+1))
done
