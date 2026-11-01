#!/usr/bin/env bash
set -euo pipefail
label=$3
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
{ go version; go env; uname -a; sha256sum "$WAGO_SPEC_INTERPRETER" "$WAGO_WAST2JSON"; } > "$out/$label-provenance.txt"
failed=0
run() {
 local name=$1 dir=$2 status=0; shift 2
 local command_text
 printf -v command_text '%q ' "$@"
 printf '%s\nworking-directory=%s\n' "${command_text% }" "$dir" > "$out/$name.command.txt"
 (cd "$source_root/$dir" && "$@") > "$out/$name.jsonl" 2> "$out/$name.stderr.log" || status=$?
 printf '%s exit=%s\n' "$name" "$status" | tee -a "$out/results.txt"
 if [ "$status" != 0 ]; then failed=1; fi
}
run "$label-tinygo-interruption" . go test -json -count=3 -timeout=30m -tags=wago_runtime,wagodebug -run '^TestTinyGoSupportsCooperativeInterruption$' ./cli/manager/internal/standalone
exit "$failed"
