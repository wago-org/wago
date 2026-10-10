#!/bin/sh
set -eu
export WAGO_SHARED_SCALAR=0 WAGO_PARITY_CORPUS=/Users/work/Code/Web/wasm.fyi/corpora/applications
go test -tags wago_codegenstats,wago_regalloccheck,wago_guardpage ./src/core/compiler/backend/railshot/arm64 > experiments/arm64-parity/constant-shift-hints-tests.txt 2>&1
go test -tags wago_guardpage -c -o /tmp/parity-constant-shift-hints.test ./experiments/arm64-parity
WAGO_PARITY_CODE_DIR=/tmp/constant-shift-hints /tmp/parity-constant-shift-hints.test -test.run=^$ -test.bench='BenchmarkParity/.*/Exec$' -test.benchtime=1x > experiments/arm64-parity/constant-shift-hints-oracles.txt 2>&1
