#!/bin/bash
set -euo pipefail
experiment_dir=/home/jtenner/Documents/Codex/2026-10-09/task-3/wago-815/evidence
for round in 1 2 3; do
  for side in baseline candidate; do
    GOMAXPROCS=2 /tmp/wago-815-$side.test -test.run='^$' -test.bench='^BenchmarkRailshotCompile(SmallScalar|MediumControl|ALUHeavy)$' -test.benchmem -test.benchtime=100x -test.count=1 > "$experiment_dir/compile-$round-$side.txt"
    GOMAXPROCS=2 /tmp/wago-815-$side.test -test.run='^$' -test.bench='^BenchmarkBoundsFactsExecute$/^sources=8$/^guard=false$' -test.benchmem -test.benchtime=10000x -test.count=1 > "$experiment_dir/exec-$round-$side.txt"
  done
done
