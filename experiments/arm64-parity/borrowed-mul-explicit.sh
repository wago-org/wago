#!/bin/sh
set -eu
export WAGO_SHARED_SCALAR=0 WAGO_ARM64_EXPERIMENT_BORROWED_MUL=1
export WAGO_PARITY_CACHE=/Users/work/Code/Web/wasm.fyi/.wasmbench/corpus-cache WAGO_PARITY_CORPUS=/Users/work/Code/Web/wasm.fyi/corpora/applications
go test -c -o /tmp/parity-borrowed-mul-explicit.test ./experiments/arm64-parity
/tmp/parity-borrowed-mul-explicit.test -test.run='^TestCachedCoreOracles$' > experiments/arm64-parity/borrowed-mul-core-explicit.txt 2>&1
/tmp/parity-borrowed-mul-explicit.test -test.run=^$ -test.bench='BenchmarkParity/.*/Exec$' -test.benchtime=1x > experiments/arm64-parity/borrowed-mul-app-explicit.txt 2>&1
go test ./src/core/compiler/backend/railshot/arm64 > experiments/arm64-parity/borrowed-mul-plain-tests.txt 2>&1
