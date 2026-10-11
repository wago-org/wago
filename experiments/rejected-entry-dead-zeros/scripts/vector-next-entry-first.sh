#!/bin/zsh
set -eu
cd /tmp/wago-vector-next-37174169
gofmt -w src/core/compiler/backend/railshot/arm64/structured_local_liveness.go src/core/compiler/backend/railshot/arm64/structured_local_liveness_arm64_test.go src/core/compiler/backend/railshot/arm64/entry_dead_zeros.go src/core/compiler/backend/railshot/arm64/entry_dead_zeros_arm64_test.go src/core/compiler/backend/railshot/arm64/compile.go src/core/compiler/backend/railshot/arm64/control.go src/core/compiler/backend/railshot/arm64/driver.go src/core/compiler/backend/railshot/arm64/knobs.go src/core/compiler/backend/railshot/arm64/policy_arm64_test.go src/core/compiler/optimization/catalog.go
go test -tags 'wago_codegenstats wago_guardpage' ./src/core/compiler/backend/railshot/arm64 -run 'TestEntryDeadZeros|TestStructured|TestHiddenOptimization' -count=1
go build -tags wago_guardpage -o /tmp/paired-entry-dead-zeros ./experiments/arm64-parity/paired
