#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/../../.."
root="$PWD"
out="$root/experiments/signed-immediate-830/pressure"
mkdir -p .tmp
backup=$(mktemp -d "$root/.tmp/store-pressure-build.XXXXXX")
amdMemory=src/core/compiler/backend/railshot/amd64/memory.go
armMemory=src/core/compiler/backend/railshot/arm64/memory.go
cp "$amdMemory" "$backup/amd64-memory.go"
cp "$armMemory" "$backup/arm64-memory.go"
restore() {
  cp "$backup/amd64-memory.go" "$amdMemory"
  cp "$backup/arm64-memory.go" "$armMemory"
  rm "$backup/amd64-memory.go" "$backup/arm64-memory.go"
  rmdir "$backup"
}
trap restore EXIT
if git apply --reverse --check "$out/mitigation.patch"; then git apply --reverse "$out/mitigation.patch"; fi
fix="$out/../correctness/fix.patch"
if git apply --reverse --check "$fix"; then
  git apply --reverse "$fix"
else
  git apply --check "$fix"
fi
for form in split early late; do
  case "$form" in
    split) binary=store-before-fix;;
    early) git apply "$fix"; binary=store-after-fix;;
    late) git apply "$out/mitigation.patch"; binary=store-late;;
  esac
  WAGO_STORE_IMM_GUARD=1 WAGO_STORE_IMM_DUMP="$out/code/$form" \
    go test -tags=wago_codegenstats,wago_regalloccheck ./src/core/compiler/backend/railshot/amd64 -run '^TestSignedImmediateStoreLoopCode$' -count=1 > "$out/$form-loop-tests.txt"
  go test -tags=wago_guardpage -c -o ".tmp/$binary-modern.test" ./src/wago
  go test -tags=wago_guardpage,wago_amd64_sse2 -c -o ".tmp/$binary-sse2.test" ./src/wago
done
