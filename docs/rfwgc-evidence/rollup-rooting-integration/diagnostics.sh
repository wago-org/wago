#!/usr/bin/env bash
set -u
export GOMAXPROCS=4
export GOROOT=/home/dev/.local/share/mise/installs/go/1.27.1
export PATH="$GOROOT/bin:$PATH"
status=0
for mode in ordinary wagodebug wago_regalloccheck; do
 tags=wago_codegenstats
 if [[ $mode != ordinary ]]; then tags=$tags,$mode; fi
 go test -p=4 -tags="$tags" ./src/core/compiler/backend/railshot/amd64 -run 'GC|Exception|Root|Lifetime|Callsite|CustomInstructionConsumes' -count=1 -v -timeout=10m > "$ROOTING_EVIDENCE_DIR/$mode-diagnostics.txt" 2>&1
 result=$?
 printf '%s diagnostics exit=%s\n' "$mode" "$result" | tee -a "$ROOTING_EVIDENCE_DIR/diagnostics-exits.txt"
 if [[ $result != 0 ]]; then status=1; fi
done
exit "$status"
