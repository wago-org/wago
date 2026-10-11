#!/bin/sh
set -eu
export WAGO_SHARED_SCALAR=0 WAGO_ARM64_EXPERIMENT_BOUNDED_SIGNED_QUOTIENT=1 WAGO_FINALIZE_VALIDATE=1
export WAGO_PARITY_CACHE=/Users/work/Code/Web/wasm.fyi/.wasmbench/corpus-cache WAGO_PARITY_CORPUS=/Users/work/Code/Web/wasm.fyi/corpora/applications
go test -tags wago_codegenstats,wago_regalloccheck ./src/core/compiler/backend/railshot/arm64 ./src/core/compiler/optimization -run '^TestBoundedSignedQuotient|TestCatalogRegistration' -count=1 -v > experiments/arm64-parity/bounded-quotient-explicit-proof-tests.txt 2>&1
go test -tags wago_guardpage,wago_codegenstats,wago_regalloccheck ./src/core/compiler/backend/railshot/arm64 -run '^TestBoundedSignedQuotientPressure' -count=1 -v > experiments/arm64-parity/bounded-quotient-pressure-proof-tests.txt 2>&1
go test -c -o /tmp/parity-bounded-quotient-explicit.test ./experiments/arm64-parity
WAGO_PARITY_CORE_CODE_DIR=/tmp/bounded-quotient-explicit-core /tmp/parity-bounded-quotient-explicit.test -test.run='^TestCachedCoreOracles$' > experiments/arm64-parity/bounded-quotient-explicit-core.txt 2>&1
WAGO_PARITY_CODE_DIR=/tmp/bounded-quotient-explicit-app /tmp/parity-bounded-quotient-explicit.test -test.run=^$ -test.bench='BenchmarkParity/.*/Exec$' -test.benchtime=1x > experiments/arm64-parity/bounded-quotient-explicit-app.txt 2>&1
