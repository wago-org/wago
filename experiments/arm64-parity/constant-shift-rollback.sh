#!/bin/sh
set -eu
export WAGO_SHARED_SCALAR=0 WAGO_PARITY_CORPUS=/Users/work/Code/Web/wasm.fyi/corpora/applications
export WAGO_PARITY_CACHE=/Users/work/Code/Web/wasm.fyi/.wasmbench/corpus-cache WAGO_PARITY_CORE_ID=wago/linked_list/sum
go test -tags wago_codegenstats,wago_regalloccheck,wago_guardpage ./src/core/compiler/backend/railshot/arm64 ./src/core/encoder/arm64 ./src/core/runtime > experiments/arm64-parity/constant-shift-rollback-tests.txt 2>&1
go test -tags wago_guardpage -c -o /tmp/parity-constant-shift-rollback.test ./experiments/arm64-parity
WAGO_PARITY_CORE_CODE_DIR=/tmp/constant-shift-core-rollback /tmp/parity-constant-shift-rollback.test -test.run='^TestCachedCoreOracles$' > experiments/arm64-parity/constant-shift-rollback-core-oracles.txt 2>&1
WAGO_PARITY_CODE_DIR=/tmp/constant-shift-rollback /tmp/parity-constant-shift-rollback.test -test.run=^$ -test.bench='BenchmarkParity/.*/Exec$' -test.benchtime=1x > experiments/arm64-parity/constant-shift-rollback-app-oracles.txt 2>&1
for image in /tmp/constant-shift-rollback/*.bin; do cmp "$image" "/tmp/store-immediate-probe/${image##*/}"; done
for image in /tmp/constant-shift-core-rollback/*.bin; do cmp "$image" "/tmp/constant-shift-core-before/${image##*/}"; done
go test ./experiments/arm64-parity -run '^TestCachedCoreOracles$' > experiments/arm64-parity/constant-shift-rollback-core-explicit.txt 2>&1
/tmp/parity-constant-shift-rollback.test -test.run=^$ -test.bench='BenchmarkCachedSimple/Exec$' -test.benchtime=500ms -test.count=3 > experiments/arm64-parity/constant-shift-rollback-linked.txt 2>&1
