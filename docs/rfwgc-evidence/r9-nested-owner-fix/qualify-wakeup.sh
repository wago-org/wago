#!/usr/bin/env bash
# Correctness qualification only; no benchmark or timing workloads.
set -euo pipefail
repo=$(git rev-parse --show-toplevel)
head=$(git rev-parse "${WAGO_QUALIFICATION_HEAD:-HEAD}")
evidence_root=$repo/docs/rfwgc-evidence/r9-nested-owner-fix
mkdir -p "$evidence_root"
evidence=${WAGO_EVIDENCE_DIR:-$(mktemp -d "$evidence_root/qualification-${head:0:9}.XXXXXX")}
if [[ -e $evidence/qualification-results.txt || -e $evidence/qualification-provenance.txt ]]; then
 echo "Refusing to overwrite qualification artifacts: $evidence" >&2
 exit 1
fi
source_tree=$(mktemp -d /tmp/wago-nested-owner-source.XXXXXX)
mkdir -p "$evidence"
git archive "$head" | tar -x -C "$source_tree"
for version in 1 2 3; do
 path=tests/conformance/spec-v$version
 pin=$(git ls-tree "$head" "$path" | awk '{print $3}')
 corpus=$repo/$path
 if ! git -C "$corpus" cat-file -e "$pin^{tree}" 2>/dev/null; then
  corpus=/home/dev/Projects/wago/$path
 fi
 git -C "$corpus" archive "$pin" | tar -x -C "$source_tree/$path"
done
export GOROOT=/home/dev/.local/share/mise/installs/go/1.27.3
export PATH=/home/dev/Projects/wago/.tools/wabt-1.0.41-linux-x64/bin:/home/dev/.local/share/mise/installs/node/26.11.1/bin:$GOROOT/bin:$PATH
export GOTOOLCHAIN=local GOWORK=off
export WAGO_SPEC_INTERPRETER=/home/dev/Projects/wago/.tools/spec-interpreter-9d36019973201a19f9c9ebb0f10828b2fe2374aa/wasm
export WAGO_SPEC_INTERPRETER_REVISION=9d36019973201a19f9c9ebb0f10828b2fe2374aa
export WAGO_WAST2JSON=/home/dev/Projects/wago/.tools/wabt-1.0.41-linux-x64/bin/wast2json WAGO_WABT_VERSION=1.0.41
unset WAGO_UPDATE_STAGED_SPEC WAGO_SPEC_FILES WAGO_SPECTEST_DIR WAGO_SPEC_VERSION
{
 printf 'head=%s\nsource=%s\n' "$head" "$source_tree"
 go version
 wat2wasm --version
 node --version
 git -C "$repo" ls-tree "$head" tests/conformance/spec-v1 tests/conformance/spec-v2 tests/conformance/spec-v3
 sha256sum "$WAGO_SPEC_INTERPRETER" "$WAGO_WAST2JSON"
} > "$evidence/qualification-provenance.txt"
cd "$source_tree"
: > "$evidence/qualification-results.txt"
run() {
 local label=$1
 shift
 printf '%q ' "$@" > "$evidence/$label.command"
 printf '\n' >> "$evidence/$label.command"
 local rc=0
 "$@" > "$evidence/$label.log" 2>&1 || rc=$?
 printf '%s exit=%s\n' "$label" "$rc" >> "$evidence/qualification-results.txt"
 # Fail closed. A failed owner assertion never authorizes a later access.
 return "$rc"
}
for mode in ordinary wagodebug wago_regalloccheck; do
 args=()
 if [[ $mode != ordinary ]]; then args=(-tags "$mode"); fi
 run "qualified-focused-$mode" go test "${args[@]}" ./src/wago -run '^TestR9Ownership' -count=1 -timeout=3m
 run "qualified-full-$mode" go test "${args[@]}" ./src/wago -count=1 -timeout=20m
 run "qualified-race-$mode" go test -race "${args[@]}" ./src/wago -run 'Test(R9Ownership|DynamicIndexedFunctionRefTestUsesBareProviderActualType|ReferenceTokensWait|FailedInstantiation|RuntimeStartTrap|GCDomainHostRoots)' -count=1 -timeout=3m
 run "qualified-backends-$mode" go test "${args[@]}" ./src/core/compiler/backend/railshot/amd64 ./src/core/compiler/backend/railshot/arm64 ./src/core/compiler/wasm -count=1 -timeout=10m
 for arch in arm64 riscv64; do
  run "qualified-cross-$arch-$mode" env GOOS=linux GOARCH="$arch" go test "${args[@]}" ./src/wago -run '^$' -exec=true
 done
done
