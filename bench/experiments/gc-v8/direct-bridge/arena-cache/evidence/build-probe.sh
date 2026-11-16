#!/bin/bash
set -euo pipefail
export PATH=toolchains/go127/bin:$PATH GOCACHE=go127-cache GOPATH=go-path
cd wago-gc-arena-cache
TAGS=wago_gc_direct_trusted_loop,wago_gc_direct_trusted_refstruct,wago_gc_direct_trusted_alloc,wago_gc_direct_fill_sse2,wago_gc_direct_copy,wago_gc_direct_array_alloc,wago_gc_direct_weighted_sum
go test -tags="$TAGS,wago_gc_direct_arena_cache,wago_codegenstats,wago_regalloccheck" ./src/wago -run TestDirectGCArenaCache -count=1 -v > gc-arena-cache/exact-tags-receipts.txt 2>&1
go test -c -tags="$TAGS" -o gc-arena-cache/control.test ./src/wago
go test -c -tags="$TAGS,wago_gc_direct_arena_cache" -o gc-arena-cache/candidate.test ./src/wago
sha256sum gc-arena-cache/*.test > gc-arena-cache/binaries.sha256
cd src/wago
for round in 0 1 2; do
 order='control candidate'; if (( round%2 )); then order='candidate control'; fi
 for label in $order; do
  GOMAXPROCS=1 GC_COMPILE_PROBE=1 gc-arena-cache/$label.test -test.run='^$' -test.bench='^BenchmarkGCDirectCompileProbe$' -test.benchtime=256x -test.count=1 > gc-arena-cache/compile-$round-$label.txt
 done
done
