#!/usr/bin/env bash
set -u
export GOMAXPROCS=4
out=docs/rfwgc-evidence/stage4-native
status=0
for mode in ordinary wagodebug wago_regalloccheck; do
  flags=()
  if [[ "$mode" != ordinary ]]; then flags=(-tags="$mode"); fi
  for suite in collector backend guest; do
    case "$suite" in
      collector) packages=(./src/core/runtime/gc/...); selector='.' ;;
      backend) packages=(./src/core/compiler/backend/railshot/amd64); selector='GC|Collector|NativeAlloc|Eden|Region' ;;
      guest) packages=(./src/wago); selector='^TestGC' ;;
    esac
    go test -p=4 "${flags[@]}" "${packages[@]}" -run "$selector" -count=1 -v -timeout=10m > "$out/$mode-$suite.txt" 2>&1
    result=$?
    printf '%s %s exit=%s\n' "$mode" "$suite" "$result"
    if [[ "$result" != 0 ]]; then status=1; fi
  done
done
exit "$status"
