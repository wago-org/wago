#!/bin/sh
set -eu
unset WAGO_ARM64_EXPERIMENT_SCOPED_LOOP_CONST
export WAGO_SHARED_SCALAR=0
export WAGO_PARITY_CACHE=/Users/work/Code/Web/wasm.fyi/.wasmbench/corpus-cache WAGO_PARITY_CORPUS=/Users/work/Code/Web/wasm.fyi/corpora/applications
go test -tags wago_codegenstats,wago_regalloccheck,wago_guardpage ./src/core/compiler/backend/railshot/arm64 ./src/core/encoder/arm64 ./src/core/runtime ./src/core/compiler/optimization > experiments/arm64-parity/scoped-loop-retain-tests.txt 2>&1
go test -tags wago_guardpage -c -o /tmp/parity-scoped-loop-retained.test ./experiments/arm64-parity
WAGO_PARITY_CORE_CODE_DIR=/tmp/scoped-loop-retained-core /tmp/parity-scoped-loop-retained.test -test.run='^TestCachedCoreOracles$' > experiments/arm64-parity/scoped-loop-retain-core.txt 2>&1
WAGO_PARITY_CODE_DIR=/tmp/scoped-loop-retained-app /tmp/parity-scoped-loop-retained.test -test.run=^$ -test.bench='BenchmarkParity/.*/Exec$' -test.benchtime=1x > experiments/arm64-parity/scoped-loop-retain-app.txt 2>&1
for image in /tmp/scoped-loop-retained-core/*.bin; do cmp "$image" "/tmp/scoped-loop-policy-core/${image##*/}"; done
for image in /tmp/scoped-loop-retained-app/*.bin; do cmp "$image" "/tmp/scoped-loop-policy-app/${image##*/}"; done
go test -tags wago_codegenstats,wago_regalloccheck ./src/core/compiler/backend/railshot/arm64 > experiments/arm64-parity/scoped-loop-retain-explicit-tests.txt 2>&1
