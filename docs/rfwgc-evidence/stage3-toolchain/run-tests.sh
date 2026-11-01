#!/usr/bin/env bash
# Run after bootstrap in an isolated archive with pinned spec-v3 clone.
set -euo pipefail
source_root=$(realpath "$1")
out=$(realpath "$2")
label=$3
export GOROOT=/home/dev/.local/share/mise/installs/go/1.27.1
export PATH="$GOROOT/bin:$source_root/.tools/wabt-1.0.41-linux-x64/bin:$PATH"
export GOMAXPROCS=2 GOFLAGS=-p=2
export WAGO_SPEC_INTERPRETER="$source_root/.tools/spec-interpreter-9d36019973201a19f9c9ebb0f10828b2fe2374aa/wasm"
export WAGO_SPEC_INTERPRETER_REVISION=9d36019973201a19f9c9ebb0f10828b2fe2374aa
export WAGO_WAST2JSON="$source_root/.tools/wabt-1.0.41-linux-x64/bin/wast2json"
export WAGO_WABT_VERSION=1.0.41
unset GOWORK WAGO_UPDATE_STAGED_SPEC WAGO_SPEC_FILES WAGO_SPECTEST_DIR
cd "$source_root"
failed=0
for mode in ordinary wagodebug wago_regalloccheck; do
  flags=()
  if [ "$mode" != ordinary ]; then flags=(-tags="$mode"); fi
  for group in converter harness staged; do
    package=./src/wago
    if [ "$group" = converter ]; then
      pattern='^TestRelease3'
      package=./tests/conformance/spectest
    elif [ "$group" = harness ]; then
      pattern='^Test(ResolveWast2JSONChecksPinnedVersion|ResolveSpecInterpreterChecksPinnedRevisionAndIdentity|ResolveSpecPlanRelease3UsesOfficialCoreLayout|SpecInterpreterModuleDefinitionInstances)$'
    else
      pattern='^TestStaged'
    fi
    cmd=(go test -count=1 -timeout=15m "${flags[@]}" -run "$pattern" -v "$package")
    printf -v command_text '%q ' "${cmd[@]}"
    printf '%s\n' "${command_text% }" > "$out/$label-$group-$mode.command.txt"
    status=0
    "${cmd[@]}" > "$out/$label-$group-$mode.log" 2>&1 || status=$?
    if [ "$status" -ne 0 ]; then failed=1; fi
    printf '%s %s %s exit=%s\n' "$label" "$group" "$mode" "$status" | tee -a "$out/results.txt"
  done
done
exit "$failed"
