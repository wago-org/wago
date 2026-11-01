#!/usr/bin/env bash
set -u
export GOROOT=/home/dev/.local/share/mise/installs/go/1.27.1
export GOMAXPROCS=4
GO="$GOROOT/bin/go"
out=docs/rfwgc-evidence/stage5-mature-review
status=0
for mode in ordinary wagodebug wago_regalloccheck; do
 flags=(); if [[ "$mode" != ordinary ]]; then flags=(-tags="$mode"); fi
 for suite in probes collector backend guest; do
  case "$suite" in
   probes) package=./src/core/runtime/gc/native; selector='^TestMatureReviewStage5' ;;
   collector) package=./src/core/runtime/gc/...; selector='.' ;;
   backend) package=./src/core/compiler/backend/railshot/amd64; selector='GC|Collector|NativeAlloc|Eden' ;;
   guest) package=./src/wago; selector='^TestGC' ;;
  esac
  "$GO" test -p=4 "${flags[@]}" "$package" -run "$selector" -count=1 -v -timeout=10m > "$out/$mode-$suite.txt" 2>&1
  result=$?
  printf '%s %s exit=%s\n' "$mode" "$suite" "$result" | tee -a "$out/exits.txt"
  if [[ "$result" != 0 ]]; then status=1; fi
 done
done
exit "$status"
