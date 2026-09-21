#!/usr/bin/env bash
set -u
root=$(git rev-parse --show-toplevel)
cd "$root"
out="$root/docs/performance/setup-cleanup/continuation"
tmp="$root/.tmp/setup-cleanup-next"
export GOMAXPROCS=16 GOGC=100 GOMEMLIMIT=off GODEBUG='' WAGO_BOUNDS=''
export PATH="/home/jtenner/.local/share/mise/installs/github-web-assembly-wabt/1.0.41/bin:$PATH"
export GOWORK="$tmp/candidate.work"
run_check() {
 name=$1; shift
 "$@" > "$out/$name.txt" 2>&1
 status=$?
 printf '%s %s\n' "$name" "$status" | tee -a "$out/final-check-status.txt"
}
run_check test-worker-diagnostic-final go test -count=1 -tags wago_guardpage ./bench/suite -run '^TestWorkerDiagnosticConfiguration$'
run_check test-worker-diagnostic-race-final go test -race -count=1 -tags wago_guardpage ./bench/suite -run '^TestWorkerDiagnosticConfiguration$'
run_check test-worker-correctness-final go test -count=1 ./internal/functionworkers ./src/core/compiler/wasm ./src/core/compiler/backend/railshot/amd64
run_check test-worker-race-final go test -race -count=1 ./internal/functionworkers ./src/core/compiler/wasm ./src/core/compiler/backend/railshot/amd64 -run 'Worker|Parallel|Invalid'
run_check test-semantic-corpus-final go test -count=1 ./bench/internal/semanticcorpus
run_check test-corpus-final go test -count=1 ./bench/suite -run '^(TestCorpus|TestCorpusSemanticExec|TestApplicationCorpusRuns|TestCatalog.*|TestValidateCorpusModule.*)$' -wago.corpus all
run_check test-corpus-guard-final go test -count=1 -tags wago_guardpage ./bench/suite -run '^(TestCorpus|TestCorpusSemanticExec|TestApplicationCorpusRuns|TestCatalog.*|TestValidateCorpusModule.*)$' -wago.corpus all
run_check test-runtime-guard-final go test -count=1 -tags wago_guardpage ./src/core/runtime
run_check test-wasi-guard-race-final go test -count=1 -race -tags wago_guardpage github.com/wago-org/wasi/internal/core github.com/wago-org/wasi/p1
run_check test-bench-isolation-race-final go test -race -count=1 -tags wago_guardpage ./bench/suite -run 'ImportLifecycle|WASIConstruction|WASIRegistration|MinimalWASI|WorkerDiagnostic'
run_check test-standalone-new-baseline env GOWORK="$tmp/baseline.work" GOFLAGS="-buildvcs=false -overlay=$tmp/baseline-overlay.json" go test -count=1 ./cli/manager/internal/standalone -run '^TestBuildTinyGoEmbedsArtifactWithoutCompiler$'
run_check test-all-final env GOFLAGS=-buildvcs=false go test -count=1 ./...
run_check lint-final just lint
