#!/usr/bin/env bash
set -u
export GOMAXPROCS=4
out=docs/rfwgc-evidence/stage4-native
status=0
for mode in ordinary wagodebug wago_regalloccheck; do
  flags=()
  tags=wago_codegenstats
  if [[ "$mode" != ordinary ]]; then flags=(-tags="$mode"); tags="$tags,$mode"; fi
  go test -p=4 "${flags[@]}" ./src/wago -run '^TestGCNativeRegions' -count=1 -v -timeout=5m > "$out/$mode-native-guests.txt" 2>&1
  result=$?
  printf '%s native-guests exit=%s\n' "$mode" "$result"
  if [[ "$result" != 0 ]]; then status=1; fi
  go test -p=4 -tags="$tags" ./src/core/compiler/backend/railshot/amd64 -run 'GC|Collector|NativeAlloc|Eden' -count=1 -v -timeout=5m > "$out/$mode-backend-diagnostics.txt" 2>&1
  result=$?
  printf '%s backend-diagnostics exit=%s\n' "$mode" "$result"
  if [[ "$result" != 0 ]]; then status=1; fi
done
exit "$status"
