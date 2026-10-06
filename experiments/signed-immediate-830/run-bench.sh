#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/../.."
root="$PWD"
out="$root/experiments/signed-immediate-830"
export GOMAXPROCS=1
cpu="${CPU:-4}"
repeats="${REPEATS:-7}"
benchtime="${BENCHTIME:-200ms}"
corpus="json-as,json-as-simd,nbody,matmul,raytrace,blake-as,blake-as-simd,sqlite3-query"
{ git rev-parse HEAD; go version; lscpu; printf 'CPU=%s REPEATS=%s BENCHTIME=%s GOMAXPROCS=1\n' "$cpu" "$repeats" "$benchtime"; sha256sum .tmp/store-{baseline,candidate}-{modern,sse2}.test .tmp/corpus-{baseline,candidate}.test; } > "$out/environment.txt"
for form in baseline candidate; do
  for profile in modern sse2 corpus; do : > "$out/$form-$profile.txt"; done
done
for ((sample=1;sample<=repeats;sample++)); do
  forms=(baseline candidate)
  profiles=(modern sse2 corpus)
  if ((sample%2==0)); then forms=(candidate baseline); profiles=(corpus sse2 modern); fi
  for profile in "${profiles[@]}"; do
    for form in "${forms[@]}"; do
      echo "sample $sample/$repeats: $profile $form"
      if [[ "$profile" == corpus ]]; then
        (cd bench/suite; taskset -c "$cpu" "$root/.tmp/corpus-$form.test" -test.run='^$' -test.bench='^(BenchmarkExec|BenchmarkCommandExec|BenchmarkCompileFull)$' -test.benchtime="$benchtime" -test.count=1 -test.timeout=15m -wago.corpus="$corpus") >> "$out/$form-corpus.txt"
      else
        (cd src/wago; taskset -c "$cpu" "$root/.tmp/store-$form-$profile.test" -test.run='^$' -test.bench='^(BenchmarkSignedImmediateStore|BenchmarkCompileSignedImmediateStore)$' -test.benchtime="$benchtime" -test.count=1 -test.timeout=15m) >> "$out/$form-$profile.txt"
      fi
    done
  done
done
for profile in modern sse2 corpus; do benchstat "$out/baseline-$profile.txt" "$out/candidate-$profile.txt" > "$out/benchstat-$profile.txt"; done
