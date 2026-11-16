#!/usr/bin/env bash
set -euo pipefail
O=/workspace/shared/gc-policy-init-final
python3 "$O/run.py" --binary-dir "$O" --output-dir "$O/audit" --audit > "$O/audit.log" 2>&1
python3 "$O/run.py" --binary-dir "$O" --output-dir "$O/full" > "$O/full.log" 2>&1
cd /workspace/shared/wago-gc-marks-baseline/src/wago
v=(baseline adaptive initheaders initadaptive)
for r in 0 1 2; do
 for i in 0 1 2 3; do
  label=${v[$(((r+i)%4))]}
  GC_COMPILE_PROBE=1 GOMAXPROCS=1 "$O/$label.test" '-test.run=^$' '-test.bench=^BenchmarkGCDirectCompileProbe$' -test.benchmem -test.benchtime=256x -test.count=1 > "$O/compile-$r-$label.txt" 2>&1
 done
done
