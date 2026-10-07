#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/../.."
root="$PWD"
out="$root/experiments/signed-immediate-830"
mkdir -p .tmp
backup=$(mktemp -d "$root/.tmp/store-build.XXXXXX")
memory=src/core/compiler/backend/railshot/amd64/memory.go
encoder=src/core/encoder/amd64/asm.go
armMemory=src/core/compiler/backend/railshot/arm64/memory.go
cp "$memory" "$backup/memory.go"
cp "$encoder" "$backup/asm.go"
cp "$armMemory" "$backup/arm64-memory.go"
restore() {
  cp "$backup/memory.go" "$memory"
  cp "$backup/asm.go" "$encoder"
  cp "$backup/arm64-memory.go" "$armMemory"
  rm "$backup/memory.go" "$backup/asm.go" "$backup/arm64-memory.go"
  rmdir "$backup"
}
trap restore EXIT

# Recreate the original optimization comparison, before the correctness fix.
# The starting production sources (including ARM64) are restored on exit.
mitigation="$out/pressure/mitigation.patch"
if git apply --reverse --check "$mitigation"; then git apply --reverse "$mitigation"; fi
fix="$out/correctness/fix.patch"
if git apply --reverse --check "$fix"; then git apply --reverse "$fix"; fi
# Only the two AMD64 production files change between these binaries.
# Refuse to proceed if these files contain other incompatible changes.
if git apply --reverse --check "$out/candidate.patch"; then
  git apply --reverse "$out/candidate.patch"
else
  git apply --check "$out/candidate.patch"
fi
for form in baseline candidate; do
  if [[ "$form" == candidate ]]; then git apply "$out/candidate.patch"; fi
  expected=split
  if [[ "$form" == candidate ]]; then expected=qword; fi
  WAGO_SIGNED_IMM64_FORM="$expected" WAGO_STORE_IMM_DUMP="$out/code/$form" \
    go test -tags=wago_codegenstats ./src/core/compiler/backend/railshot/amd64 -run '^TestSignedImmediateStore(Selection|LoopCode)$/guard=false' -count=1 > "$out/$form-selection.txt"
  go test ./src/wago -run '^TestSignedImmediateStore' -count=1 > "$out/$form-runtime-tests.txt"
  go test -c -o ".tmp/store-$form-modern.test" ./src/wago
  go test -tags=wago_amd64_sse2 -c -o ".tmp/store-$form-sse2.test" ./src/wago
  (cd bench/suite; go test -c -o "$root/.tmp/corpus-$form.test" .)
  go run -tags=wago_codegenstats ./experiments/signed-immediate-830/inspect > "$out/$form-corpus.csv"
done
