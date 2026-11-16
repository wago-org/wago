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
cd /workspace/shared/wago-gc-recovered/src/wago
GOMAXPROCS=1 GC_BRIDGE_CASE=struct-allocation-access GC_BRIDGE_MODE=direct GC_BRIDGE_COMPILER_MODE=baseline GC_BRIDGE_AGE2=0 GC_BRIDGE_EDEN=2097152 GC_BRIDGE_WARMUP=128 GC_BRIDGE_SAMPLES=1 GC_BRIDGE_BATCH=512 GC_BRIDGE_ALLOW_ZERO_COLLECTION=0 /workspace/shared/gc-integrated-full/baseline.test '-test.run=^TestGCDirectBridgeProbe$' -test.v -test.count=1 -test.cpuprofile="$O/struct-current.cpu.pprof" > "$O/struct-current.run.txt" 2>&1
export GOCACHE="$O/gocache" GOROOT=/workspace/shared/toolchains/go127 GOTOOLCHAIN=local
"$GOROOT/bin/go" tool pprof -top /workspace/shared/gc-integrated-full/baseline.test "$O/struct-current.cpu.pprof" > "$O/struct-current.flat.txt"
"$GOROOT/bin/go" tool pprof -top -cum /workspace/shared/gc-integrated-full/baseline.test "$O/struct-current.cpu.pprof" > "$O/struct-current.cum.txt"
