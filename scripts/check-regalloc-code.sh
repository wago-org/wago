#!/usr/bin/env bash
# Compare exact native fixture guest bytes in ordinary and checked builds.
set -euo pipefail
cd "$(dirname "$0")/.."
tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT
arch=$(go env GOARCH)
case "$arch" in
  amd64) package=./src/core/compiler/backend/railshot/amd64 ;;
  arm64) package=./src/core/compiler/backend/railshot/arm64 ;;
  *) echo 'code-image fingerprint fixture requires amd64 or arm64' >&2; exit 2 ;;
esac
for variant in ordinary checked; do
  tags=(-tags=)
  [[ "$variant" == ordinary ]] || tags=(-tags=wago_regalloccheck)
  go test "${tags[@]}" -count=1 -run '^TestRegallocCheckEmissionFingerprint$' -v "$package" > "$tmp/$variant.log"
  grep -o 'REGALLOC_CODE .*' "$tmp/$variant.log" > "$tmp/$variant.code"
done
diff -u "$tmp/ordinary.code" "$tmp/checked.code"
cat "$tmp/ordinary.code"
