#!/bin/sh
set -eu
export WAGO_SHARED_SCALAR=0 WAGO_PARITY_CORPUS=/Users/work/Code/Web/wasm.fyi/corpora/applications
go test -tags wago_codegenstats,wago_regalloccheck,wago_guardpage ./src/core/compiler/backend/railshot/arm64 > experiments/arm64-parity/constant-floor-rollback-tests.txt 2>&1
go test -tags wago_guardpage -c -o /tmp/parity-constant-floor-rollback.test ./experiments/arm64-parity
WAGO_PARITY_CODE_DIR=/tmp/constant-floor-rollback /tmp/parity-constant-floor-rollback.test -test.run=^$ -test.bench='BenchmarkParity/.*/Exec$' -test.benchtime=1x > experiments/arm64-parity/constant-floor-rollback-oracles.txt 2>&1
for image in /tmp/constant-floor-rollback/*.bin; do
 cmp "$image" "/tmp/store-immediate-probe/${image##*/}"
done
