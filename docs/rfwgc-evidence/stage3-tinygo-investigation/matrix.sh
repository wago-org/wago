#!/usr/bin/env bash
set -euo pipefail
root=$(realpath "${1:-.}")
e="$root/docs/rfwgc-evidence/stage3-tinygo-investigation"
local_tools=/home/dev/.local/share/wago-tinygo-investigation
export GOROOT=/home/dev/.local/share/mise/installs/go/1.27.1
export PATH="$local_tools/wrapper:$GOROOT/bin:$PATH"
export GOTOOLCHAIN=local GOMAXPROCS=2 GOFLAGS=-p=2
unset GOWORK
export REAL_TINYGO="$local_tools/tinygo/bin/tinygo"
for source in current baseline; do
 if [ "$source" = current ]; then tree="$root/.tools/stage3-full-suite/current"; else tree="$root/.tools/stage3-toolchain/baseline"; fi
 for mode in ordinary wagodebug wago_regalloccheck; do
  label="042-$source-$mode"
  export TINYGO_CAPTURE="$local_tools/artifacts/$label"
  tags=wago_runtime
  if [ "$mode" != ordinary ]; then tags="$tags,$mode"; fi
  rc=0
  (cd "$tree"; go test -json -count=1 -timeout=20m -tags="$tags" -run '^TestTinyGoSupportsCooperativeInterruption$' ./cli/manager/internal/standalone) > "$e/$label.jsonl" 2> "$e/$label.stderr" || rc=$?
  echo "$label exit=$rc" | tee -a "$e/results.txt"
 done
done
