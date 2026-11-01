#!/usr/bin/env bash
set -eu
export GOMAXPROCS=4
out=docs/rfwgc-evidence/stage5-mature-runtime
for mode in ordinary wagodebug wago_regalloccheck; do
 tags=wago_codegenstats
 if [[ "$mode" != ordinary ]]; then tags="$tags,$mode"; fi
 go test -p=4 -tags="$tags" ./src/core/compiler/backend/railshot/amd64 -run 'GC|Collector|NativeAlloc|Eden' -count=1 -v -timeout=5m > "$out/$mode-backend-diagnostics.txt" 2>&1
done
