#!/bin/sh
set -eu
export WAGO_SHARED_SCALAR=0 WAGO_ARM64_EXPERIMENT_SCOPED_LOOP_CONST=1
export WAGO_PARITY_CACHE=/Users/work/Code/Web/wasm.fyi/.wasmbench/corpus-cache
export WAGO_PARITY_CORPUS=/Users/work/Code/Web/wasm.fyi/corpora/applications
go test -c -o /tmp/parity-scoped-loop-const-explicit.test ./experiments/arm64-parity
/tmp/parity-scoped-loop-const-explicit.test -test.run='^TestCachedCoreOracles$' > experiments/arm64-parity/scoped-loop-const-core-explicit.txt 2>&1
/tmp/parity-scoped-loop-const-explicit.test -test.run=^$ -test.bench='BenchmarkParity/.*/Exec$' -test.benchtime=1x > experiments/arm64-parity/scoped-loop-const-app-explicit.txt 2>&1
