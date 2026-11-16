#!/usr/bin/env bash
set -euo pipefail
cd /workspace/shared/wago-gc-marks-baseline
export GOROOT=/workspace/shared/toolchains/go127 GOTOOLCHAIN=local GOCACHE=/workspace/shared/gc-marks-evidence/gocache
export GOPATH=/workspace/shared/go-path GOMODCACHE=/workspace/shared/go-path/pkg/mod
T=wago_gc_direct_trusted_loop,wago_gc_direct_trusted_refstruct,wago_gc_direct_trusted_alloc,wago_gc_direct_fill_sse2,wago_gc_direct_copy,wago_gc_direct_array_alloc,wago_gc_direct_weighted_sum,wago_gc_retire_adaptive
"$GOROOT/bin/go" test -p 1 -c -tags "$T" -o /workspace/shared/gc-adaptive-integrated/adaptive.test ./src/wago
