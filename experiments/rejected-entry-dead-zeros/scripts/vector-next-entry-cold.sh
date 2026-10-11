#!/bin/zsh
set -eu
cd /tmp/wago-vector-next-37174169
export WAGO_SHARED_SCALAR=0
export WAGO_LOCAL_INIT_PROOF_MODULE=/Users/work/Code/Web/wasm.fyi/.wasmbench/corpus-cache/artifacts/a59c88869766c6f6fe1e35c9505b3fd9f83d347499e3b674b1c8abae15b59fd1.wasm
gofmt -w src/core/compiler/backend/railshot/arm64/compile.go src/core/compiler/backend/railshot/arm64/entry_dead_zeros.go src/core/compiler/backend/railshot/arm64/entry_dead_zeros_arm64_test.go src/core/compiler/backend/railshot/arm64/structured_local_initialization.go
go test -tags 'wago_codegenstats wago_regalloccheck wago_guardpage' ./src/core/compiler/backend/railshot/arm64 -run 'TestEntryDeadZeros|TestStructured|TestInitialLocalReads|TestHiddenOptimization' -count=1 -v > /tmp/entry-dead-zeros-cold-fixtures.txt
go build -tags wago_guardpage -o /tmp/paired-entry-dead-zeros-cold ./experiments/arm64-parity/paired
go test -c -tags wago_guardpage -o /tmp/parity-entry-dead-zeros-cold.test ./experiments/arm64-parity
go build -tags wago_guardpage -o /tmp/inventory-entry-dead-zeros-cold experiments/arm64-parity/entry-dead-zeros-inventory.go
for mode in signals explicit; do
  env WAGO_BOUNDS=$mode WAGO_ARM64_EXPERIMENT_ENTRY_DEAD_ZEROS=1 /tmp/inventory-entry-dead-zeros-cold /tmp/entry-dead-zeros-cold-$mode-on
done
env WAGO_BOUNDS=signals WAGO_ARM64_EXPERIMENT_ENTRY_DEAD_ZEROS=0 /tmp/inventory-entry-dead-zeros-cold /tmp/entry-dead-zeros-cold-signals-off
python3 /tmp/vector-next-entry-scratch-identity.py cold
