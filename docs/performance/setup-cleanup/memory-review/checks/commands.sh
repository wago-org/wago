#!/usr/bin/env bash
set -u
root='/tmp/wago-setup-cleanup-pr-uvyppyeh/wago'
provider='/tmp/wago-setup-cleanup-pr-uvyppyeh/wasi-publish'
out='/tmp/wago memory review/checks'
mkdir -p "$out"
export PATH="/home/jtenner/.local/share/mise/installs/github-web-assembly-wabt/1.0.41/bin:$PATH"
export GOMAXPROCS=16 WAGO_BOUNDS= GOGC=100 GOMEMLIMIT=off GODEBUG=
run() {
 name=$1;shift
 "$@" > "$out/$name.txt" 2>&1
 status=$?
 printf '%s %s\n' "$name" "$status" | tee -a "$out/status.txt"
}
cd "$root"
for version in 1.22.12 1.27.1; do
 export GOROOT="/home/jtenner/.local/share/mise/installs/go/$version" GOTOOLCHAIN=local
 go="$GOROOT/bin/go"
 export GOWORK='/tmp/wago-setup-cleanup-pr-uvyppyeh/publish.work'
 run "wago-$version" "$go" test -count=1 ./src/wago
 run "wago-guard-$version" "$go" test -count=1 -tags wago_guardpage ./src/wago
 run "wago-import-race-$version" "$go" test -race -count=1 ./src/wago -run 'Import|Host|InstanceClose'
 run "bench-race-$version" "$go" test -race -count=1 -tags wago_guardpage ./bench/suite -run 'ImportLifecycle|WASIConstruction|WASIRegistration|WorkerDiagnostic'
 run "corpus-$version" "$go" test -count=1 -tags wago_guardpage ./bench/suite -run '^(TestCorpus|TestCorpusSemanticExec|TestApplicationCorpusRuns|TestCatalog.*|TestValidateCorpusModule.*)$' -wago.corpus all
 cd "$provider"
 unset GOWORK
 run "provider-$version" "$go" test -race -count=1 ./...
 run "provider-vet-$version" "$go" vet ./...
 cd "$root"
done
export GOROOT='/home/jtenner/.local/share/mise/installs/go/1.27.1'
export GOWORK='/tmp/wago-setup-cleanup-pr-uvyppyeh/publish.work'
run runtime-guard go test -count=1 -tags wago_guardpage ./src/core/runtime
run worker-failure-race go test -race -count=1 ./internal/functionworkers ./src/core/compiler/wasm ./src/core/compiler/backend/railshot/amd64 -run 'Worker|Parallel|Invalid'
run wasi-fuzz go test -tags wago_guardpage ./bench/suite -run '^$' -fuzz '^FuzzWASIConstructionIsolation$' -fuzztime 10s
run script-tests python3 docs/performance/setup-cleanup/test_measurement_scripts.py
run lint just lint
