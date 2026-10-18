#!/usr/bin/env bash
# Host-independent guest fingerprints and force-linked ordinary RV64 DCE.
set -euo pipefail
cd "$(dirname "$0")/.."
test "$(go env GOOS)" = linux
check_dir=$(mktemp -d)
trap 'rm -rf -- "$check_dir"' EXIT
package=./src/core/compiler/backend/railshot/riscv64
for variant in ordinary checked; do
  tags=(-tags=)
  [[ "$variant" == ordinary ]] || tags=(-tags=wago_regalloccheck)
  go test "${tags[@]}" -count=1 -run '^(TestRegallocCheckEmissionFingerprint|TestProductionFloatStaticRoundingAndFingerprint)$' -v "$package" > "$check_dir/$variant.log"
  rg 'REGALLOC_CODE |FLOAT_CODE ' "$check_dir/$variant.log" > "$check_dir/$variant.code"
done
diff -u "$check_dir/ordinary.code" "$check_dir/checked.code"
cat "$check_dir/ordinary.code"
for arch in amd64 riscv64; do
  CGO_ENABLED=0 GOOS=linux GOARCH="$arch" GORISCV64=rva20u64 \
    go build -o "$check_dir/ordinary-$arch" ./scripts/testdata/diagnostic-rv64-dce
  go tool nm "$check_dir/ordinary-$arch" > "$check_dir/symbols-$arch"
  if rg -e ' T .*internal/regalloccheck\.' \
    -e ' T .*encoder/riscv64\..*(regalloc|ObserveGPWrites|ObserveRegalloc)' \
    -e ' T .*railshot/riscv64\..*check(Immutable|EndLifetimes|TerminalGPWrites|RestoreGPWrites)' \
    "$check_dir/symbols-$arch"; then
    echo "ordinary RV64 diagnostics retained in linux/$arch harness" >&2
    exit 1
  fi
  rg -q ' T .*railshot/riscv64\.(compileOptions|compileFunction|compileInteger)' "$check_dir/symbols-$arch"
  rg -q ' T .*encoder/riscv64\.' "$check_dir/symbols-$arch"
  echo "linux/$arch force-linked RV64: diagnostic implementation symbols absent"
done
