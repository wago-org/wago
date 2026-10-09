#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/../.."
root=$PWD
out="$root/experiments/loop-unroll-vectorization/results"
base=2286d676facdfa1cabe2d1c61072e505438ca7f2
baseline_dir=$(mktemp -d)
trap 'rm -rf "$baseline_dir"' EXIT
git archive "$base" | tar -x -C "$baseline_dir"
# The same benchmark harness measures both compiler versions. It adds counts,
# without changing source Wasm or native invocation.
cp src/core/compiler/backend/railshot/amd64/linear_sum_loop_amd64_bench_test.go "$baseline_dir/src/core/compiler/backend/railshot/amd64/"
python3 - "$baseline_dir" <<'PY'
import pathlib, sys
prefix = pathlib.Path('src/core/compiler/backend/railshot/amd64')
s = (prefix / 'experimental_replication_test.go').read_text()
rejected = s[s.index('func BenchmarkExperimentalRejectedCompile'):s.index('func BenchmarkExperimentalReduction')]
s = (prefix / 'experimental_evidence_test.go').read_text()
helpers = s[s.index('func experimentCorpusModule'):s.index('func TestExperimentalCoverage')]
corpus = s[s.index('func BenchmarkExperimentalCorpusCompile'):s.index('func TestExperimentalSettings')]
source = 'package amd64\nimport ("os";"path/filepath";"testing";"github.com/wago-org/wago/src/core/compiler/wasm")\n' + helpers + rejected + corpus
source = source.replace('\tmode := os.Getenv("WAGO_LOOP_REPLICATION")\n', '').replace('ExperimentalLoopMode: mode, ', '')
(pathlib.Path(sys.argv[1]) / prefix / 'experimental_baseline_bench_test.go').write_text(source)
PY
mkdir -p "$baseline_dir/experiments/loop-unroll-vectorization/cmd/workload"
cp experiments/loop-unroll-vectorization/cmd/workload/main.go "$baseline_dir/experiments/loop-unroll-vectorization/cmd/workload/"
cd "$baseline_dir"
GOWORK=off go test -c -o "$out/baseline.test" ./src/core/compiler/backend/railshot/amd64
GOWORK=off go build -o "$out/workload-baseline" ./experiments/loop-unroll-vectorization/cmd/workload
