#!/usr/bin/env bash
set -euo pipefail
repo=$(git rev-parse --show-toplevel)
evidence="$repo/docs/rfwgc-evidence/tinygo-foreign-stack-fix"
source_tree=$(sed -n 's/^source=//p' "$evidence/qualification-provenance.txt")
cache=$(sed -n 's/^fresh_cache=//p' "$evidence/qualification-provenance.txt")
export GOROOT=/home/dev/.local/share/mise/installs/go/1.27.1
export PATH="$GOROOT/bin:/home/dev/Projects/wago/.tools/wabt-1.0.41-linux-x64/bin:/home/dev/.local/share/mise/installs/github-bytecodealliance-wasm-tools/1.251.0:$PATH"
export GOTOOLCHAIN=local GOMAXPROCS=2 GOFLAGS=-p=2 GOWORK=off
export XDG_CACHE_HOME="$cache" GOCACHE="$cache/go-build"
export WAGO_SPEC_INTERPRETER="$repo/.tools/stage3-full-suite/current/.tools/spec-interpreter-9d36019973201a19f9c9ebb0f10828b2fe2374aa/wasm"
export WAGO_SPEC_INTERPRETER_REVISION=9d36019973201a19f9c9ebb0f10828b2fe2374aa
export WAGO_WAST2JSON=/home/dev/Projects/wago/.tools/wabt-1.0.41-linux-x64/bin/wast2json WAGO_WABT_VERSION=1.0.41
unset WAGO_UPDATE_STAGED_SPEC WAGO_SPEC_FILES WAGO_SPECTEST_DIR WAGO_SPEC_VERSION
{
 wat2wasm --version
 wasm-tools --version
 sha256sum "$WAGO_SPEC_INTERPRETER" "$WAGO_WAST2JSON"
} > "$evidence/host-tools-provenance.txt"
cd "$source_tree"
for mode in ordinary wagodebug wago_regalloccheck; do
 tags=wago_runtime
 if [[ $mode != ordinary ]]; then tags+=",$mode"; fi
 rc=0
 timeout 30m go test -count=1 -timeout=25m -tags="$tags" ./src/core/runtime ./src/wago > "$evidence/host-pinned-$mode.log" 2>&1 || rc=$?
 printf 'host-pinned-%s exit=%s\n' "$mode" "$rc" >> "$evidence/qualification-reruns.txt"
done
