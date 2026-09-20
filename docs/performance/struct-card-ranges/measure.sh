#!/usr/bin/env bash
set -euo pipefail
# Build both test binaries before running. Pass absolute binary paths.
base=${1:?baseline test binary}
fix=${2:?optimized test binary}
out=${3:?output directory}
mkdir -p "$out"
export GOMAXPROCS=1
: > "$out/baseline.txt"
: > "$out/optimized.txt"
for pair in 1 2 3 4 5 6; do
  order='baseline optimized'
  if (( pair % 2 == 0 )); then order='optimized baseline'; fi
  for version in $order; do
    binary=$base
    if [[ $version == optimized ]]; then binary=$fix; fi
    printf 'pair=%s version=%s time=%s\n' "$pair" "$version" "$(date -u +%FT%TZ)"
    taskset -c "${BENCH_CPU:-2}" "$binary" -test.run '^$' \
      -test.bench "${BENCH_PATTERN:-^BenchmarkGCStructCard}" -test.benchmem \
      -test.benchtime "${BENCH_TIME:-200ms}" -test.count 1 >> "$out/$version.txt"
  done
done
