#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/../../.."
root="$PWD"
out="$root/experiments/signed-immediate-830/correctness"
export GOMAXPROCS=1
cpu="${CPU:-4}"
repeats="${REPEATS:-7}"
benchtime="${BENCHTIME:-150ms}"
{ go version; printf 'CPU=%s REPEATS=%s BENCHTIME=%s GOMAXPROCS=1\n' "$cpu" "$repeats" "$benchtime"; sha256sum .tmp/store-{before,after}-fix-{modern,sse2}.test; } > "$out/environment.txt"
for form in before after; do
  for profile in modern sse2; do : > "$out/$form-$profile.txt"; done
done
for ((sample=1;sample<=repeats;sample++)); do
  forms=(before after)
  profiles=(modern sse2)
  if ((sample%2==0)); then forms=(after before); profiles=(sse2 modern); fi
  for profile in "${profiles[@]}"; do
    for form in "${forms[@]}"; do
      echo "sample $sample/$repeats: $profile $form"
      (cd src/wago; taskset -c "$cpu" "$root/.tmp/store-$form-fix-$profile.test" -test.run='^$' -test.bench='^(BenchmarkSignedImmediateStore|BenchmarkGuardSignedImmediateStore)$' -test.benchtime="$benchtime" -test.count=1 -test.timeout=15m) >> "$out/$form-$profile.txt"
    done
  done
done
for profile in modern sse2; do benchstat "$out/before-$profile.txt" "$out/after-$profile.txt" > "$out/benchstat-$profile.txt"; done
