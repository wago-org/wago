#!/bin/sh
set -eu
export WAGO_SHARED_SCALAR=0
export WAGO_PARITY_CORPUS=/Users/work/Code/Web/wasm.fyi/corpora/applications
go test -tags wago_codegenstats,wago_regalloccheck,wago_guardpage ./src/core/compiler/backend/railshot ./src/core/compiler/backend/railshot/arm64 ./src/core/runtime ./src/core/encoder/arm64 ./src/core/compiler/optimization > experiments/arm64-parity/store-map-removal-tests.txt 2>&1
go test -tags wago_guardpage -c -o /tmp/parity-store-map-removed.test ./experiments/arm64-parity
go test -c -o /tmp/parity-store-map-removed-explicit.test ./experiments/arm64-parity
WAGO_PARITY_CODE_DIR=/tmp/store-map-removed /tmp/parity-store-map-removed.test -test.run=^$ -test.bench='BenchmarkParity/.*/Exec$' -test.benchtime=1x > experiments/arm64-parity/store-map-removal-signal-oracles.txt 2>&1
/tmp/parity-store-map-removed-explicit.test -test.run=^$ -test.bench='BenchmarkParity/.*/Exec$' -test.benchtime=1x > experiments/arm64-parity/store-map-removal-explicit-oracles.txt 2>&1
for image in /tmp/store-immediate32-retained/*.bin; do
 cmp "$image" "/tmp/store-map-removed/${image##*/}"
done
