#!/bin/zsh
set -eu
cd /tmp/wago-vector-next-37174169
git sparse-checkout add /corpus/
export WAGO_SHARED_SCALAR=0
export WAGO_PARITY_CORPUS=/Users/work/Code/Web/wasm.fyi/corpora/applications
export WAGO_PARITY_CACHE=/Users/work/Code/Web/wasm.fyi/.wasmbench/corpus-cache
export WAGO_ARM64_EXPERIMENT_ENTRY_DEAD_ZEROS=1
export WAGO_LOCAL_INIT_PROOF_MODULE=/Users/work/Code/Web/wasm.fyi/.wasmbench/corpus-cache/artifacts/a59c88869766c6f6fe1e35c9505b3fd9f83d347499e3b674b1c8abae15b59fd1.wasm
gofmt -w src/core/compiler/backend/railshot/arm64/structured_local_liveness_arm64_test.go src/core/compiler/backend/railshot/arm64/compile.go
go test -tags 'wago_codegenstats wago_regalloccheck wago_guardpage' ./src/core/compiler/backend/railshot/arm64 -run 'TestEntryDeadZeros|TestStructured|TestInitialLocalReads|TestHiddenOptimization' -count=1 -v > /tmp/entry-dead-zeros-extra-fixtures.txt
go test -tags 'wago_codegenstats wago_regalloccheck wago_guardpage' ./src/core/compiler/backend/railshot/arm64 ./src/core/compiler/optimization > /tmp/entry-dead-zeros-diagnostics.txt
go build -tags wago_guardpage -o /tmp/paired-entry-dead-zeros-qualified ./experiments/arm64-parity/paired
go test -c -tags wago_guardpage -o /tmp/parity-entry-dead-zeros.test ./experiments/arm64-parity
go test -c -o /tmp/parity-entry-dead-zeros-explicit.test ./experiments/arm64-parity
export WAGO_BOUNDS=signals
export WAGO_PARITY_CODE_DIR=/tmp/entry-dead-zeros-qual-signals-app
export WAGO_PARITY_CORE_CODE_DIR=/tmp/entry-dead-zeros-qual-signals-core
/tmp/parity-entry-dead-zeros.test -test.run '^TestCachedCoreOracles$' -test.v > /tmp/entry-dead-zeros-qual-signals-core.txt
/tmp/parity-entry-dead-zeros.test -test.run '^$' -test.bench 'BenchmarkParity/.*/Exec$' -test.benchtime=1x > /tmp/entry-dead-zeros-qual-signals-app.txt
export WAGO_BOUNDS=explicit
export WAGO_PARITY_CODE_DIR=/tmp/entry-dead-zeros-qual-explicit-app
export WAGO_PARITY_CORE_CODE_DIR=/tmp/entry-dead-zeros-qual-explicit-core
/tmp/parity-entry-dead-zeros-explicit.test -test.run '^TestCachedCoreOracles$' -test.v > /tmp/entry-dead-zeros-qual-explicit-core.txt
/tmp/parity-entry-dead-zeros-explicit.test -test.run '^$' -test.bench 'BenchmarkParity/.*/Exec$' -test.benchtime=1x > /tmp/entry-dead-zeros-qual-explicit-app.txt
