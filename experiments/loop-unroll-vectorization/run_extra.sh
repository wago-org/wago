#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/../.."
out=${1:-experiments/loop-unroll-vectorization/results/final}
for key in ${!WAGO_@}; do unset "$key"; done
for pattern in 'corpus-execute-*.txt' 'modern-*.txt' commands.jsonl followup; do
  if compgen -G "$out/$pattern" >/dev/null; then
    echo "Choose an output directory without extra samples. Existing samples are preserved." >&2
    exit 1
  fi
done
mkdir -p "$out/followup"
go test -c -o "$out/extra.test" ./src/core/compiler/backend/railshot/amd64
go build -o experiments/loop-unroll-vectorization/results/workload ./experiments/loop-unroll-vectorization/cmd/workload
export GOMAXPROCS=1
for round in 1 2 3 4 5 6; do
  if (( round % 2 )); then order='scalar count2 count4 guard2'; else order='guard2 count4 count2 scalar'; fi
  for mode in $order; do
    selected=$mode
    [[ $mode != scalar ]] || selected=''
    WAGO_LOOP_REPLICATION="$selected" "$out/extra.test" -test.run '^$' -test.bench '^BenchmarkExperimentalCorpusExecute$' -test.benchmem -test.benchtime 100ms >> "$out/corpus-execute-$mode.txt"
  done
  if (( round % 2 )); then order='scalar vector-f32 vector-i32'; else order='vector-i32 vector-f32 scalar'; fi
  for mode in $order; do
    selected=$mode
    [[ $mode != scalar ]] || selected=''
    WAGO_LOOP_FEATURES=modern WAGO_LOOP_REPLICATION="$selected" "$out/extra.test" -test.run '^$' -test.bench '^BenchmarkExperimentalReplication$/(map-i32|map-f32)/compile$' -test.benchmem -test.benchtime 100ms >> "$out/modern-$mode.txt"
    WAGO_LOOP_FEATURES=modern WAGO_LOOP_REPLICATION="$selected" "$out/extra.test" -test.run '^$' -test.bench '^BenchmarkExperimentalReplication$/(map-i32|map-f32)/execute$' -test.benchmem -test.benchtime 50ms >> "$out/modern-$mode.txt"
  done
done
python3 experiments/loop-unroll-vectorization/run_commands.py "$out/commands.jsonl"
for round in $(seq 1 12); do
  if (( round % 2 )); then order='base new'; else order='new base'; fi
  for variant in $order; do
    binary="$out/extra.test"
    [[ $variant != base ]] || binary=experiments/loop-unroll-vectorization/results/baseline.test
    "$binary" -test.run '^$' -test.bench '^BenchmarkExperimentalRejectedCompile$/large$' -test.benchmem -test.benchtime 200ms >> "$out/followup/default-large-$variant.txt"
  done
done
benchstat "$out/followup/default-large-"{base,new}.txt > "$out/followup/benchstat.txt"
