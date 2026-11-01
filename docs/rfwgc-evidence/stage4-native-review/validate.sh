#!/usr/bin/env bash
set -uo pipefail
here=$(cd -- "$(dirname -- "$0")" && pwd)
export GOROOT=/home/dev/.local/share/mise/installs/go/1.27.1 GOMAXPROCS=4
export PATH="$GOROOT/bin:$PATH"
: > "$here/exits.txt"
for mode in ordinary wagodebug wago_regalloccheck; do
 tags=(); diagnostic=wago_codegenstats
 if [[ $mode != ordinary ]]; then tags=(-tags="$mode"); diagnostic="$mode,wago_codegenstats"; fi
 cd "$here/snapshot"
 go test -p=4 "${tags[@]}" ./src/core/runtime/gc/... -count=1 -v -timeout=10m > "$here/$mode-collector.txt" 2>&1
 echo "$mode collector $?" >> "$here/exits.txt"
 go test -p=4 "${tags[@]}" ./src/wago -run 'TestNativeReviewStage4|TestGCNativeRegions|TestGCEdenRegion' -count=1 -v -timeout=10m > "$here/$mode-guests.txt" 2>&1
 echo "$mode guests $?" >> "$here/exits.txt"
 go test -p=4 -tags="$diagnostic" ./src/core/compiler/backend/railshot/amd64 -run 'GC|Collector|NativeAlloc|Eden|Region' -count=1 -v -timeout=5m > "$here/$mode-backend.txt" 2>&1
 echo "$mode backend $?" >> "$here/exits.txt"
 cd "$here/baseline"
 go test -p=4 -tags="$diagnostic" ./src/core/compiler/backend/railshot/amd64 -run '^TestIntervalRegion(DynamicReuse|ControlBoundaries)$' -count=1 -v -timeout=5m > "$here/$mode-baseline-interval.txt" 2>&1
 echo "$mode baseline-interval $?" >> "$here/exits.txt"
done
cd "$here/snapshot"
GOOS=linux GOARCH=arm64 go test -c -o "$here/arm64-backend.test" ./src/core/compiler/backend/railshot/arm64 > "$here/arm64-build.txt" 2>&1
echo "arm64 backend compile $?" >> "$here/exits.txt"
GOOS=linux GOARCH=arm64 go test -c -o "$here/arm64-wago.test" ./src/wago >> "$here/arm64-build.txt" 2>&1
echo "arm64 wago compile $?" >> "$here/exits.txt"
