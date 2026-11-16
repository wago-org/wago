#!/bin/bash
set -euo pipefail
EXP_ROOT="$(cd "${GC_EXPERIMENT_ROOT:-.}" && pwd)"
export PATH=$EXP_ROOT/toolchains/go127/bin:$PATH GOCACHE=$EXP_ROOT/go127-cache GOPATH=$EXP_ROOT/go-path
cd $EXP_ROOT/wago-gc-arena-cache
TAGS=wago_gc_direct_trusted_loop,wago_gc_direct_trusted_refstruct,wago_gc_direct_trusted_alloc,wago_gc_direct_fill_sse2,wago_gc_direct_copy,wago_gc_direct_array_alloc,wago_gc_direct_weighted_sum
go test -tags="$TAGS,wago_gc_direct_arena_cache,wago_codegenstats,wago_regalloccheck" ./src/wago -run TestDirectGCArenaCache -count=1 -v > $EXP_ROOT/gc-arena-cache/exact-tags-receipts.txt 2>&1
go test -c -tags="$TAGS" -o $EXP_ROOT/gc-arena-cache/control.test ./src/wago
go test -c -tags="$TAGS,wago_gc_direct_arena_cache" -o $EXP_ROOT/gc-arena-cache/candidate.test ./src/wago
(cd "$EXP_ROOT/gc-arena-cache" && sha256sum *.test > binaries.sha256)
cd src/wago
for round in 0 1 2; do
 order='control candidate'; if (( round%2 )); then order='candidate control'; fi
 for label in $order; do
  GOMAXPROCS=1 GC_COMPILE_PROBE=1 $EXP_ROOT/gc-arena-cache/$label.test -test.run='^$' -test.bench='^BenchmarkGCDirectCompileProbe$' -test.benchtime=256x -test.count=1 > $EXP_ROOT/gc-arena-cache/compile-$round-$label.txt
 done
done
