#!/usr/bin/env bash
set -euo pipefail
REPO=/workspace/shared/wago-gc-recovered
OUT=/workspace/shared/gc-integrated-full
P=$REPO/bench/experiments/gc-v8/direct-bridge/direct16-pair-range
cd "$REPO"
python3 "$P/run.py" --binary-dir "$OUT" --output-dir "$OUT/audit" --audit > "$OUT/audit.log" 2>&1
python3 "$P/full-v8.py" --binary-dir "$OUT" --output-dir "$OUT/full" --node node > "$OUT/full.log" 2>&1
cd "$REPO/src/wago"
variants=(baseline paired range)
for round in 0 1 2; do
  for offset in 0 1 2; do
    variant=${variants[$(((round+offset)%3))]}
    GC_COMPILE_PROBE=1 GOMAXPROCS=1 "$OUT/$variant.test" '-test.run=^$' '-test.bench=^BenchmarkGCDirectCompileProbe$' -test.benchmem -test.benchtime=256x -test.count=1 > "$OUT/compile-$round-$variant.txt" 2>&1
  done
done
