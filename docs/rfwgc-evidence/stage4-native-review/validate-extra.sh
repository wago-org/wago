#!/usr/bin/env bash
set -uo pipefail
here=$(cd -- "$(dirname -- "$0")" && pwd)
export GOROOT=/home/dev/.local/share/mise/installs/go/1.27.1 GOMAXPROCS=4
export PATH="$GOROOT/bin:$PATH"
cd "$here/snapshot"
: > "$here/extra-exits.txt"
for mode in ordinary wagodebug wago_regalloccheck; do
 tags=(); if [[ $mode != ordinary ]]; then tags=(-tags="$mode"); fi
 go test -p=4 "${tags[@]}" ./src/core/runtime/gc/native ./src/wago -run '^TestNativeReviewStage4|^TestCompiledHasNoArtifactCodec$|^TestLoadRejectsRetiredArtifact$' -count=1 -v -timeout=5m > "$here/$mode-probes-artifacts.txt" 2>&1
 echo "$mode probes-artifacts $?" >> "$here/extra-exits.txt"
done
