#!/bin/zsh
set -eu
cd /tmp/wago-vector-next-37174169
export WAGO_SHARED_SCALAR=0
gofmt -w src/core/compiler/backend/railshot/arm64/structured_local_liveness.go src/core/compiler/backend/railshot/arm64/structured_local_liveness_arm64_test.go experiments/arm64-parity/entry-dead-zeros-inspect.go
go test -tags 'wago_codegenstats wago_regalloccheck wago_guardpage' ./src/core/compiler/backend/railshot/arm64 -run 'TestEntryDeadZeros|TestStructured|TestHiddenOptimization' -count=1
go build -tags wago_guardpage -o /tmp/paired-entry-dead-zeros-bulk ./experiments/arm64-parity/paired
go run -tags 'wago_codegenstats wago_guardpage' experiments/arm64-parity/entry-dead-zeros-inspect.go /Users/work/Code/Web/wasm.fyi/.wasmbench/corpus-cache/artifacts/a59c88869766c6f6fe1e35c9505b3fd9f83d347499e3b674b1c8abae15b59fd1.wasm > /tmp/entry-dead-zeros-bulk-counts.txt
/tmp/paired-entry-dead-zeros-bulk -option entry-dead-zeros -core-exec -corpus /Users/work/Code/Web/wasm.fyi/.wasmbench/corpus-cache -workloads modular-arithmetic -rounds 8 -budget 200ms -code-dir /tmp/native-entry-dead-zeros-bulk-core > /tmp/entry-dead-zeros-bulk-core-exec.jsonl
/tmp/paired-entry-dead-zeros-bulk -option entry-dead-zeros -core-compile -phase compile -corpus /Users/work/Code/Web/wasm.fyi/.wasmbench/corpus-cache -workloads modular-arithmetic -rounds 8 -budget 100ms > /tmp/entry-dead-zeros-bulk-core-compile.jsonl
