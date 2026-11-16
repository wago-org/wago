#!/usr/bin/env bash
set -euo pipefail
O=/workspace/shared/gc-marks-evidence
for v in baseline candidate; do GOMAXPROCS=1 "$O/$v.test" '-test.run=.' -test.count=1 > "$O/tests-$v.txt" 2>&1; done
for r in 0 1 2 3 4; do
 if ((r%2==0)); then variants='baseline candidate'; else variants='candidate baseline'; fi
 for v in $variants; do
  GOMAXPROCS=1 "$O/$v.test" '-test.run=^$' '-test.bench=^BenchmarkMarkCleanupGraphs$' -test.benchtime=300ms -test.benchmem -test.count=1 > "$O/bench-$r-$v.txt" 2>&1
 done
done
