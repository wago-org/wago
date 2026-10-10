#!/bin/sh
set -eu
export WAGO_SHARED_SCALAR=0 WAGO_ARM64_EXPERIMENT_SCOPED_LOOP_CONST=1
export WAGO_PARITY_CACHE=/Users/work/Code/Web/wasm.fyi/.wasmbench/corpus-cache WAGO_PARITY_CORPUS=/Users/work/Code/Web/wasm.fyi/corpora/applications
go test -tags wago_codegenstats,wago_regalloccheck,wago_guardpage ./src/core/compiler/backend/railshot/arm64 ./src/core/encoder/arm64 ./src/core/runtime > experiments/arm64-parity/scoped-loop-const-tests.txt 2>&1
go test -tags wago_codegenstats,wago_regalloccheck,wago_guardpage ./experiments/arm64-parity -run '^TestCachedCoreOracles$' -v > experiments/arm64-parity/scoped-loop-const-core-diagnostics.txt 2>&1
go test -tags wago_guardpage -c -o /tmp/parity-scoped-loop-const.test ./experiments/arm64-parity
WAGO_PARITY_CORE_CODE_DIR=/tmp/scoped-loop-const-core /tmp/parity-scoped-loop-const.test -test.run='^TestCachedCoreOracles$' > experiments/arm64-parity/scoped-loop-const-core-oracles.txt 2>&1
WAGO_PARITY_CODE_DIR=/tmp/scoped-loop-const /tmp/parity-scoped-loop-const.test -test.run=^$ -test.bench='BenchmarkParity/.*/Exec$' -test.benchtime=1x > experiments/arm64-parity/scoped-loop-const-app-oracles.txt 2>&1
