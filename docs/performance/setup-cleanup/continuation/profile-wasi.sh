#!/usr/bin/env bash
set -euo pipefail
root=$(git rev-parse --show-toplevel)
out="$root/docs/performance/setup-cleanup/continuation"
tmp="$root/.tmp/setup-cleanup-next"
export GOMAXPROCS=16 GOGC=100 GOMEMLIMIT=off GODEBUG='' WAGO_BOUNDS=signals
cd "$root/bench/suite"
for label in baseline candidate; do
 taskset -c 0-15 "$tmp/wasi-$label.test" -test.run '^$' -test.bench '^BenchmarkCommandLifecycleDiagnostic$/^minimal-wasi$/^Imports$' -test.benchtime 10000x -test.memprofile "$tmp/imports-$label.alloc" -test.memprofilerate 1 -wago.bench.lifecycle -wago.corpus cjson > "$out/imports-$label-alloc-run.txt" 2>&1
 for metric in alloc_objects alloc_space; do
  go tool pprof -top -sample_index "$metric" "$tmp/wasi-$label.test" "$tmp/imports-$label.alloc" > "$out/imports-$label-$metric.txt"
  go tool pprof -list 'binding.*callback|Plugin.*bindings|Imports.*HostFunc' -sample_index "$metric" "$tmp/wasi-$label.test" "$tmp/imports-$label.alloc" > "$out/imports-$label-$metric-sites.txt"
 done
 taskset -c 0-15 "$tmp/wasi-$label.test" -test.run '^$' -test.bench '^BenchmarkCommandLifecycleDiagnostic$/^minimal-wasi$/^Imports$' -test.benchtime 3s -test.cpuprofile "$tmp/imports-$label.cpu" -wago.bench.lifecycle -wago.corpus cjson > "$out/imports-$label-cpu-run.txt" 2>&1
 go tool pprof -top "$tmp/wasi-$label.test" "$tmp/imports-$label.cpu" > "$out/imports-$label-cpu.txt"
done
