#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/../../.."
root="$PWD"
out="$root/experiments/signed-immediate-830/pressure"
export GOMAXPROCS=1
cpu="${CPU:-4}"
repeats="${REPEATS:-7}"
benchtime="${BENCHTIME:-200ms}"
declare -A binaries=(
  [split-modern]="store-before-fix-modern.test" [split-sse2]="store-before-fix-sse2.test"
  [early-modern]="store-after-fix-modern.test" [early-sse2]="store-after-fix-sse2.test"
  [late-modern]="store-late-modern.test" [late-sse2]="store-late-sse2.test"
)
{ go version; printf 'CPU=%s REPEATS=%s BENCHTIME=%s GOMAXPROCS=1\n' "$cpu" "$repeats" "$benchtime"; sha256sum .tmp/store-{before,after}-fix-{modern,sse2}.test .tmp/store-late-{modern,sse2}.test; } > "$out/environment.txt"
for form in split early late; do
  for profile in modern sse2; do : > "$out/$form-$profile.txt"; done
done
for ((sample=1;sample<=repeats;sample++)); do
  # Rotate which form runs first; reverse direction every other sample.
  case $((sample%3)) in
    1) forms=(split early late);;
    2) forms=(early late split);;
    0) forms=(late split early);;
  esac
  profiles=(modern sse2)
  if ((sample%2==0)); then forms=("${forms[2]}" "${forms[1]}" "${forms[0]}"); profiles=(sse2 modern); fi
  for profile in "${profiles[@]}"; do
    for form in "${forms[@]}"; do
      echo "sample $sample/$repeats: $profile $form"
      (cd src/wago; taskset -c "$cpu" "$root/.tmp/${binaries[$form-$profile]}" -test.run='^$' -test.bench='^(BenchmarkSignedImmediateStore|BenchmarkGuardSignedImmediateStore)$/(ones|near-positive|near-negative|mixed-halves|pressure|pressure-near-miss)$' -test.benchtime="$benchtime" -test.count=1 -test.timeout=15m) >> "$out/$form-$profile.txt"
    done
  done
done
for profile in modern sse2; do
  benchstat "$out/early-$profile.txt" "$out/late-$profile.txt" > "$out/benchstat-early-late-$profile.txt"
  benchstat "$out/split-$profile.txt" "$out/late-$profile.txt" > "$out/benchstat-split-late-$profile.txt"
done
