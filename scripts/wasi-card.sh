#!/usr/bin/env sh
# Emit the "WASI preview 1" section fragment for the CI card: pass/fail/skip
# counts from TestWASISuite (the WebAssembly/wasi-testsuite preview1 tests run
# through wago.WASI). Fragment format matches the other producers: line 1 is the
# <summary>, the rest is the body. Degrades to a placeholder when the tests/wasi
# submodule is unavailable.
set -eu

report="${WASI_REPORT:-ci-card/wasi.md}"

root=$(git rev-parse --show-toplevel) || {
	printf 'wago: not inside a git repository\n' >&2
	exit 1
}
cd "$root"

placeholder() {
	printf 'WASI preview 1 — _%s_\n\n_No data._\n' "$1" >"$report"
	printf 'WASI preview 1 — %s\n' "$1"
	exit 0
}

probe=tests/wasi/tests/rust/testsuite/wasm32-wasip1/big_random_buf.wasm
[ -f "$probe" ] || git submodule update --init tests/wasi >/dev/null 2>&1 || true
[ -f "$probe" ] || placeholder "tests/wasi submodule not present"

line=$(WAGO_WASITEST_DIR="$root/tests/wasi" \
	go test -count=1 -run TestWASISuite -v ./plugins/wasi/p1/ 2>/dev/null \
	| grep -oE "TOTAL\[wasip1\]: passed=[0-9]+ failed=[0-9]+ skipped=[0-9]+ \(of [0-9]+\)" || true)

passed=$(printf '%s' "$line" | sed -nE 's/.*passed=([0-9]+).*/\1/p'); passed=${passed:-0}
failed=$(printf '%s' "$line" | sed -nE 's/.*failed=([0-9]+).*/\1/p'); failed=${failed:-0}
skipped=$(printf '%s' "$line" | sed -nE 's/.*skipped=([0-9]+).*/\1/p'); skipped=${skipped:-0}
total=$(printf '%s' "$line" | sed -nE 's/.*\(of ([0-9]+)\)$/\1/p'); total=${total:-0}
attempted=$((passed + failed))

summary="WASI preview 1: ${passed}/${attempted} passed · ${skipped} skipped (of ${total} wasm32-wasip1 tests)"
body=$(printf '| Snapshot | Passed | Failed | Skipped | Total |\n|---|---|---|---|---|\n| wasm32-wasip1 | %s | %s | %s | %s |' \
	"$passed" "$failed" "$skipped" "$total")

printf '%s\n%s\n' "$summary" "$body" >"$report"
printf '%s\n' "$summary"
