#!/usr/bin/env bash
set -euo pipefail
out=docs/performance/setup-cleanup
export GOMAXPROCS=16
go test -count=1 ./src/wago > "$out/test-wago.txt" 2>&1
go test -count=1 -tags wago_guardpage ./src/wago > "$out/test-wago-guard.txt" 2>&1
go test -race -count=1 ./src/wago -run 'Test(Imports|ImportSnapshot|InstantiateHostImport|InstantiateSnapshotsImports|CompiledMapsExecutable|ConcurrentInstantiate)' > "$out/test-imports-race.txt" 2>&1
go test -count=1 github.com/wago-org/wasi/... > "$out/test-wasi.txt" 2>&1
go test -race -count=1 github.com/wago-org/wasi/internal/core github.com/wago-org/wasi/p1 > "$out/test-wasi-race.txt" 2>&1
