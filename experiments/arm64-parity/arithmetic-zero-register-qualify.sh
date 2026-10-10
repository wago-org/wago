#!/bin/sh
set -eu
export WAGO_SHARED_SCALAR=0 WAGO_ARM64_EXPERIMENT_ARITHMETIC_ZERO=1 WAGO_PARITY_CACHE=/Users/work/Code/Web/wasm.fyi/.wasmbench/corpus-cache
 go test -tags wago_codegenstats,wago_regalloccheck,wago_guardpage ./src/core/compiler/backend/railshot/arm64 ./src/core/compiler/optimization ./src/core/encoder/arm64 > experiments/arm64-parity/arithmetic-zero-register-tests.txt 2>&1
 go test -tags wago_guardpage -c -o /tmp/parity-arithmetic-zero-register.test ./experiments/arm64-parity
 WAGO_PARITY_CORE_CODE_DIR=/tmp/arithmetic-zero-register-core /tmp/parity-arithmetic-zero-register.test -test.run='^TestCachedCoreOracles$' > experiments/arm64-parity/arithmetic-zero-register-core.txt 2>&1
