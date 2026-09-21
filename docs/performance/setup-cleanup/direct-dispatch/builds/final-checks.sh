#!/usr/bin/env bash
set -u
root='/tmp/wago-direct-dispatch-osuyzuzw'
export GOWORK="$root/build.work" GOMAXPROCS=16 GOGC=100 GOMEMLIMIT=off GODEBUG= WAGO_BOUNDS=
export PATH="/home/jtenner/.local/share/mise/installs/github-web-assembly-wabt/1.0.41/bin:$PATH"
cd "$root/wago"
run() { local name=$1;shift; "$@" > "$root/checks/$name.txt" 2>&1; local status=$?; printf '%s %s\n' "$name" "$status" | tee -a "$root/checks/status.txt"; }
run corpus go test -count=1 -tags wago_guardpage -overlay "$root/overlay.json" ./bench/suite -run '^(TestCorpus|TestCorpusSemanticExec|TestApplicationCorpusRuns|TestCatalog.*|TestValidateCorpusModule.*)$' -wago.corpus all
run fuzz go test -tags wago_guardpage -overlay "$root/overlay.json" ./bench/suite -run '^$' -fuzz '^FuzzWASIConstructionIsolation$' -fuzztime 10s
cd bench/suite
export WAGO_BOUNDS=signals
run execution-allocations taskset -c 0-15 "$root/wasi-candidate.test" -test.run '^$' -test.bench '^BenchmarkExec$' -test.benchtime 100x -test.benchmem -wago.corpus all
