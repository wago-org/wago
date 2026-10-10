#!/bin/sh
set -eu
cd /tmp/wago-borrowed-div-rem-isolated-20261010
export GOWORK=off WAGO_SHARED_SCALAR=0 WAGO_ARM64_EXPERIMENT_LEAF_SCOPED_CONST=0 WAGO_ARM64_EXPERIMENT_BORROWED_DIV_REM=1
export WAGO_PARITY_CACHE=/Users/work/Code/Web/wasm.fyi/.wasmbench/corpus-cache WAGO_PARITY_CORPUS=/Users/work/Code/Web/wasm.fyi/corpora/applications
RESULTS=/Users/work/Code/Wago/wago/experiments/arm64-parity
go test -c -o /tmp/parity-borrowed-div-rem-isolated-explicit.test ./experiments/arm64-parity
/tmp/parity-borrowed-div-rem-isolated-explicit.test -test.run='^TestCachedCoreOracles$' > "$RESULTS/borrowed-div-rem-isolated-core-explicit.txt" 2>&1
/tmp/parity-borrowed-div-rem-isolated-explicit.test -test.run=^$ -test.bench='BenchmarkParity/.*/Exec$' -test.benchtime=1x > "$RESULTS/borrowed-div-rem-isolated-app-explicit.txt" 2>&1
go test ./src/core/compiler/backend/railshot/arm64 > "$RESULTS/borrowed-div-rem-isolated-plain-tests.txt" 2>&1
