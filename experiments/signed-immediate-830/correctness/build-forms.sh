#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/../../.."
root="$PWD"
out="$root/experiments/signed-immediate-830/correctness"
mkdir -p .tmp
backup=$(mktemp -d "$root/.tmp/store-fix-build.XXXXXX")
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
# Both forms retain the signed-immediate optimization. Only fix.patch differs.
mitigation="$out/../pressure/mitigation.patch"
if git apply --reverse --check "$mitigation"; then git apply --reverse "$mitigation"; fi
if git apply --reverse --check "$out/fix.patch"; then
  git apply --reverse "$out/fix.patch"
else
  git apply --check "$out/fix.patch"
fi
for form in before after; do
  if [[ "$form" == after ]]; then git apply "$out/fix.patch"; fi
  WAGO_STORE_IMM_GUARD=1 WAGO_STORE_IMM_DUMP="$out/code/$form" \
    go test -tags=wago_codegenstats ./src/core/compiler/backend/railshot/amd64 -run '^TestSignedImmediateStoreLoopCode$' -count=1 > "$out/$form-loop-tests.txt"
  go test -tags=wago_guardpage -c -o ".tmp/store-$form-fix-modern.test" ./src/wago
  go test -tags=wago_guardpage,wago_amd64_sse2 -c -o ".tmp/store-$form-fix-sse2.test" ./src/wago
done
