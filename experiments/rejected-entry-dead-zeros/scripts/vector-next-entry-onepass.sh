#!/bin/zsh
set -eu
cd /tmp/wago-vector-next-37174169
export WAGO_SHARED_SCALAR=0
export WAGO_LOCAL_INIT_PROOF_MODULE=/Users/work/Code/Web/wasm.fyi/.wasmbench/corpus-cache/artifacts/a59c88869766c6f6fe1e35c9505b3fd9f83d347499e3b674b1c8abae15b59fd1.wasm
gofmt -w src/core/compiler/backend/railshot/arm64/structured_local_initialization.go src/core/compiler/backend/railshot/arm64/structured_local_liveness_reference_arm64_test.go src/core/compiler/backend/railshot/arm64/structured_local_liveness_arm64_test.go src/core/compiler/backend/railshot/arm64/entry_dead_zeros.go src/core/compiler/backend/railshot/arm64/compile.go
go test -tags 'wago_codegenstats wago_regalloccheck wago_guardpage' ./src/core/compiler/backend/railshot/arm64 -run 'TestEntryDeadZeros|TestStructured|TestInitialLocalReads|TestHiddenOptimization' -count=1 -v > /tmp/entry-dead-zeros-onepass-fixtures.txt
go build -tags wago_guardpage -o /tmp/paired-entry-dead-zeros-onepass ./experiments/arm64-parity/paired
/tmp/paired-entry-dead-zeros-onepass -option entry-dead-zeros -core-exec -corpus /Users/work/Code/Web/wasm.fyi/.wasmbench/corpus-cache -workloads modular-arithmetic -rounds 10 -budget 250ms -code-dir /tmp/native-entry-dead-zeros-onepass-core > /tmp/entry-dead-zeros-onepass-core-exec.jsonl
/tmp/paired-entry-dead-zeros-onepass -option entry-dead-zeros -core-compile -phase compile -corpus /Users/work/Code/Web/wasm.fyi/.wasmbench/corpus-cache -workloads modular-arithmetic -rounds 8 -budget 100ms > /tmp/entry-dead-zeros-onepass-core-compile.jsonl
