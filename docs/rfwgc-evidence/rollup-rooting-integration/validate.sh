#!/usr/bin/env bash
set -u
export GOMAXPROCS=4
export GOROOT=/home/dev/.local/share/mise/installs/go/1.27.1
export PATH="$GOROOT/bin:$PATH"
out=${ROOTING_EVIDENCE_DIR:-docs/rfwgc-evidence/rollup-rooting-integration}
status=0
for mode in ordinary wagodebug wago_regalloccheck; do
 flags=(); if [[ $mode != ordinary ]]; then flags=(-tags="$mode"); fi
 for suite in guest backend runtime; do
  case $suite in
   guest) packages=(./src/wago); selector='^Test(GC|PreferredGCCollector|Exception|StagedCrossInstanceException|CompiledHasNoArtifactCodec|Load.*|.*Artifact.*Reject|.*Reject.*Artifact|.*Lifetime)' ;;
   backend) packages=(./src/core/compiler/backend/railshot/amd64 ./src/core/compiler/backend/railshot/shared ./src/core/compiler/frontend); selector='GC|Exception|Root|Callsite|Lifetime|CustomInstructionConsumes' ;;
   runtime) packages=(./src/core/runtime); selector='Stack|Frame|Host|Reentry|Native' ;;
  esac
  go test -p=4 "${flags[@]}" "${packages[@]}" -run "$selector" -count=1 -v -timeout=10m > "$out/$mode-$suite.txt" 2>&1
  result=$?
  printf '%s %s exit=%s\n' "$mode" "$suite" "$result" | tee -a "$out/exits.txt"
  if [[ $result != 0 ]]; then status=1; fi
 done
done
exit "$status"
