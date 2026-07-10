#!/usr/bin/env sh
set -eu

root=$(git rev-parse --show-toplevel)
tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT HUP INT TERM

SPEC_LOG_DIR="$root/scripts/testdata/spec-card" \
SPEC_REPORT="$tmp/spec.md" \
	"$root/scripts/spec-card.sh" >/dev/null

diff -u "$root/scripts/testdata/spec-card/expected.md" "$tmp/spec.md"

mkdir "$tmp/missing-total"
cp "$root/scripts/testdata/spec-card/1.0.log" "$tmp/missing-total/1.0.log"
cp "$root/scripts/testdata/spec-card/3.0.log" "$tmp/missing-total/3.0.log"
printf '%s\n' 'go test failed before accounting' >"$tmp/missing-total/2.0.log"
SPEC_LOG_DIR="$tmp/missing-total" SPEC_REPORT="$tmp/missing.md" \
	"$root/scripts/spec-card.sh" >/dev/null
grep -q 'suite 2.0 produced no total accounting line' "$tmp/missing.md"
