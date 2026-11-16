#!/bin/bash
set -euo pipefail
ROOT=$(git rev-parse --show-toplevel)
cd "$ROOT"
OUT=${OUT:-artifacts/fresh-struct-forward}
mkdir -p "$OUT"
OUT=$(cd "$OUT" && pwd)
TAGS=wago_gc_fresh_struct_probe,wago_gc_direct_trusted_loop,wago_gc_direct_trusted_refstruct,wago_gc_direct_trusted_alloc,wago_gc_direct_fill_sse2,wago_gc_direct_copy,wago_gc_direct_array_alloc,wago_gc_direct_weighted_sum
go test ./src/core/compiler/optimization ./src/core/compiler/backend/railshot/amd64 ./src/wago -count=1 > "$OUT/default.txt" 2>&1
go test -tags="$TAGS,wago_codegenstats,wago_regalloccheck" ./src/core/compiler/optimization ./src/wago -run 'TestDirectGCFreshStruct|TestGCFreshStructExactArtifactReceipt|TestGCFreshStructSameBinaryReceipts|TestFreshStructProbe|TestCatalog' -v -count=1 > "$OUT/active.txt" 2>&1
go test -race -tags="$TAGS" ./src/wago -run TestDirectGCFreshStructPolicyIsolation -v -count=1 > "$OUT/race.txt" 2>&1
go test -c -tags="$TAGS" -o "$OUT/probe.test" ./src/wago
(cd "$OUT" && sha256sum probe.test > binary.sha256)
