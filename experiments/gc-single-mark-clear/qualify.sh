#!/usr/bin/env bash
# Prepared only: execute after CPU release. Preserve every row, do not mask failures.
set -uo pipefail
cd wago-gc-single-mark-clear
export PATH=toolchains/go127/bin:$PATH
export GOROOT=toolchains/go127 GOTOOLCHAIN=local
export GOCACHE=go127-cache GOPATH=go-path
export GOMODCACHE=go-path/pkg/mod XDG_CACHE_HOME=tinygo-cache
O=gc-single-clear-qualification
mkdir -p "$O"; : > "$O/status.txt"
run(){ local n=$1;shift; "$@" > "$O/$n.log" 2>&1; local code=$?; printf '%s %s\n' "$n" "$code" | tee -a "$O/status.txt"; }
run core go test -p 1 -count=1 ./experiments/directgc ./experiments/compactgc
run core-race go test -p 1 -race -count=1 ./experiments/directgc ./experiments/compactgc
for tag in wago_gc_retire_startmap wago_gc_retire_adaptive; do run "$tag" go test -p 1 -count=1 -tags="$tag" ./experiments/directgc; done
for tag in wago_gc_compact_age2 wago_gc_compact_eden wago_gc_compact_age2,wago_gc_compact_eden wago_gc_compact_cohort wago_gc_compact_shadow wago_gc_compact_age2,wago_gc_compact_age2_metrics wago_gc_compact_age2,wago_gc_compact_age2_queue_reuse wago_gc_compact8; do
 run "$tag" go test -p 1 -count=1 -tags="$tag" ./experiments/compactgc
done
run coexist-race go test -p 1 -race -count=1 -tags=wago_gc_compact_age2,wago_gc_compact_eden ./experiments/compactgc
run default-runtime go test -p 1 -count=1 ./src/wago ./src/core/runtime/gc/native ./src/core/runtime/gc ./src/core/compiler/backend/railshot/amd64
T=wago_gc_direct_trusted_loop,wago_gc_direct_trusted_refstruct,wago_gc_direct_trusted_alloc,wago_gc_direct_fill_sse2,wago_gc_direct_copy,wago_gc_direct_array_alloc,wago_gc_direct_weighted_sum,wago_regalloccheck,wago_codegenstats
run direct-runtime go test -p 1 -count=1 -tags="$T" ./src/wago -run '^(TestDirectGC|TestTrustedDirect)'
run direct-backend go test -p 1 -count=1 -tags="$T" ./src/core/compiler/backend/railshot/amd64 -run '^(TestDirectLoop|TestGCModeBuild)'
run tinygo-default env GOCACHE=tinygo-cache toolchains/tinygo/bin/tinygo test -scheduler=tasks ./experiments/directgc ./experiments/compactgc
run tinygo-coexist env GOCACHE=tinygo-cache toolchains/tinygo/bin/tinygo test -scheduler=tasks -tags=wago_gc_compact_eden,wago_gc_compact_age2 ./experiments/compactgc
awk '$2 != 0 {bad=1} END {exit bad}' "$O/status.txt"
