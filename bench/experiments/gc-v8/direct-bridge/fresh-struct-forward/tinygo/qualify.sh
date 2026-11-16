#!/usr/bin/env bash
set -uo pipefail
cd "$(git rev-parse --show-toplevel)"
TINYGO=${TINYGO:-tinygo}
OUT=${OUT:-$(mktemp -d)}
mkdir -p "$OUT"
run(){ local name=$1; shift; "$@" > "$OUT/$name.log" 2>&1; local code=$?; printf '%s %s\n' "$name" "$code" | tee -a "$OUT/status.txt"; }
: > "$OUT/status.txt"
run version "$TINYGO" version
run core "$TINYGO" test -scheduler=tasks ./experiments/directgc ./experiments/compactgc
run backend "$TINYGO" test -scheduler=tasks -tags=wago_regalloccheck ./src/core/compiler/backend/railshot/amd64
run portable "$TINYGO" test -scheduler=tasks -tags=wago_regalloccheck ./internal/regalloccheck ./src/core/encoder/amd64 ./src/core/encoder/arm64
run runtime "$TINYGO" test -scheduler=tasks -tags=wago_regalloccheck ./src/core/runtime ./src/wago
# To reproduce backend-control.log, run the backend command in a separate
# worktree at 1cb2b993. Configure writable Go/TinyGo caches in the environment.
