#!/usr/bin/env bash
set -euo pipefail
base=${1:?main binary}
previous=${2:?previous PR binary}
fix=${3:?candidate binary}
out=${4:?output directory}
mkdir -p "$out"
export GOMAXPROCS=1
for version in baseline previous optimized; do : > "$out/$version.txt"; done
for round in 1 2 3 4 5 6; do
  order='baseline previous optimized'
  if (( round % 2 == 0 )); then order='optimized previous baseline'; fi
  for version in $order; do
    binary=$base
    if [[ $version == previous ]]; then binary=$previous; fi
    if [[ $version == optimized ]]; then binary=$fix; fi
    printf 'round=%s version=%s time=%s\n' "$round" "$version" "$(date -u +%FT%TZ)"
    taskset -c "${BENCH_CPU:-2}" "$binary" -test.run '^$' \
      -test.bench "${BENCH_PATTERN:-^BenchmarkGCStructCardScanControls$/^fields=4097$/^ranges=33$}" \
      -test.benchmem -test.benchtime "${BENCH_TIME:-5s}" -test.count 1 >> "$out/$version.txt"
  done
done
