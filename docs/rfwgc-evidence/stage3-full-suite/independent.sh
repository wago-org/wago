#!/usr/bin/env bash
set -euo pipefail
source_root=$(realpath "$1")
out=$(realpath "$2")
export GOROOT=/home/dev/.local/share/mise/installs/go/1.27.1
export PATH="$GOROOT/bin:$source_root/.tools/wabt-1.0.41-linux-x64/bin:$PATH"
export GOMAXPROCS=2 GOFLAGS=-p=2 GOTOOLCHAIN=local
export DOTNET_SYSTEM_GLOBALIZATION_INVARIANT=1
export WAGO_SPEC_INTERPRETER="$source_root/.tools/spec-interpreter-9d36019973201a19f9c9ebb0f10828b2fe2374aa/wasm"
export WAGO_SPEC_INTERPRETER_REVISION=9d36019973201a19f9c9ebb0f10828b2fe2374aa
export WAGO_WAST2JSON="$source_root/.tools/wabt-1.0.41-linux-x64/bin/wast2json" WAGO_WABT_VERSION=1.0.41
unset GOWORK WAGO_UPDATE_STAGED_SPEC WAGO_SPEC_FILES WAGO_SPECTEST_DIR WAGO_SPEC_VERSION
cd "$source_root"
{ go version; go env; uname -a; sha256sum "$WAGO_SPEC_INTERPRETER" "$WAGO_WAST2JSON"; } > "$out/provenance.txt"
failed=0
run() {
 local name=$1 dir=$2 status=0; shift 2
 printf '%q ' "$@" > "$out/$name.command.txt"; printf '\nworking-directory=%s\n' "$dir" >> "$out/$name.command.txt"
 (cd "$source_root/$dir" && "$@") > "$out/$name.jsonl" 2> "$out/$name.stderr.log" || status=$?
 printf '%s exit=%s\n' "$name" "$status" | tee -a "$out/results.txt"
 if [ "$status" != 0 ]; then failed=1; fi
}
# Run only after run.sh completes, to retain the global two-package bound.
for mode in ordinary wagodebug wago_regalloccheck; do
 flags=(); if [ "$mode" != ordinary ]; then flags=(-tags="$mode"); fi
 run "root-$mode-retry" . go test -json -count=1 -timeout=30m "${flags[@]}" ./...
done
run race . go test -json -race -count=1 -timeout=30m ./src/wago ./src/core/runtime ./tests/integration/runtimeconcurrency
for mode in wagodebug wagodebug,wago_guardpage; do
 run "gc-hardening-$mode" . go test -json -count=3 -timeout=30m -tags="$mode" ./src/core/runtime/gc/...
 run "gc-host-$mode" . go test -json -count=3 -timeout=30m -tags="$mode" -run 'Test(RuntimeGCHostFuncRefCallAndTailOwnership|GenericGCResultsIssueBoundedHostTokens|GCCrossInstanceCallsWithSharedPersistentRoots|GCHostReentryNativeFrameRoots|GCArm64HostReentryRoots|GCWarmSnapshotPreservesCyclesAndSharing)$' ./src/wago
done
run guard . go test -json -count=1 -timeout=30m -tags=wago_regalloccheck,wago_guardpage ./src/core/runtime ./src/wago
run corpus-all bench go test -json -count=1 -timeout=30m -tags=wago_regalloccheck -run '^(TestCorpus|TestCorpusSemanticExec|TestCatalog.*|TestValidateCorpusModule.*|TestApplicationCorpusRuns)$' ./suite -args -wago.corpus=all
WAGO_SPECTEST_DIR="$source_root/tests/conformance/spec-v1" WAGO_SPEC_VERSION=1.0 run spec-v1 . go test -json -count=1 -timeout=30m -tags=wago_regalloccheck -run TestSpecSuiteExec ./src/wago
WAGO_SPECTEST_DIR="$source_root/tests/conformance/spec-v3" WAGO_SPEC_VERSION=3.0 run spec-v3 . go test -json -count=1 -timeout=30m -tags=wago_regalloccheck -run TestSpecSuiteExec ./src/wago
WAGO_SPECTEST_DIR="$source_root/tests/conformance/spec-v3" WAGO_SPEC_VERSION=3.0 WAGO_BOUNDS=signals run spec-v3-signals . go test -json -count=1 -timeout=30m -tags=wago_regalloccheck,wago_guardpage -run TestSpecSuiteExec ./src/wago
exit "$failed"
