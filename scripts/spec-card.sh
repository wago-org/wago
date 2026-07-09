#!/usr/bin/env sh
# Emit the "WebAssembly spec" section fragment for the CI card: per-version
# pass/skip counts from TestSpecSuiteExec (the preserved 1.0 MVP baseline, the
# independently pinned official 2.0 core suite, and the 3.0 proposal aggregate).
# Fragment format matches the other producers: line 1 is the <summary>, the rest
# is the body. Degrades to a placeholder when wast2json or a required corpus is
# unavailable.
set -eu

report="${SPEC_REPORT:-ci-card/spec.md}"

root=$(git rev-parse --show-toplevel) || {
	printf 'wago: not inside a git repository\n' >&2
	exit 1
}
cd "$root"

placeholder() {
	printf 'WebAssembly spec — _%s_\n\n_No data._\n' "$1" >"$report"
	printf 'WebAssembly spec — %s\n' "$1"
	exit 0
}

command -v wast2json >/dev/null 2>&1 || placeholder "wast2json (wabt) not installed"
[ -f tests/spec/i32.wast ] || git submodule update --init tests/spec >/dev/null 2>&1 || true
[ -f tests/spec/i32.wast ] || placeholder "tests/spec submodule not present"
[ -f tests/spec-v2/test/core/i32.wast ] || git submodule update --init tests/spec-v2 >/dev/null 2>&1 || true
[ -f tests/spec-v2/test/core/i32.wast ] || placeholder "tests/spec-v2 submodule not present"

rows=""
summary=""
for v in 1.0 2.0 3.0; do
	case "$v" in
		2.0) suite="$root/tests/spec-v2" ;;
		*)   suite="$root/tests/spec" ;;
	esac
	line=$(WAGO_SPECTEST_DIR="$suite" WAGO_SPEC_VERSION="$v" \
		go test -count=1 -run TestSpecSuiteExec -v ./src/wago/ 2>/dev/null \
		| grep -oE "TOTAL\[$v\]: assertions passed=[0-9]+ \| skipped modules=[0-9]+ skipped assertions=[0-9]+" || true)
	passed=$(printf '%s' "$line" | sed -nE 's/.*passed=([0-9]+).*/\1/p'); passed=${passed:-0}
	smod=$(printf '%s' "$line" | sed -nE 's/.*modules=([0-9]+).*/\1/p'); smod=${smod:-0}
	sass=$(printf '%s' "$line" | sed -nE 's/.*assertions=([0-9]+)$/\1/p'); sass=${sass:-0}
	# Pass rate = passed / (passed + skipped assertions): the share of that
	# version's testsuite assertions wago runs and gets right.
	pct=$(awk -v p="$passed" -v s="$sass" 'BEGIN { t=p+s; printf (t>0 ? "%.1f" : "0.0"), (t>0 ? 100*p/t : 0) }')
	case "$v" in
		1.0) label="1.0 (MVP core)" ;;
		2.0) label="2.0 (release core)" ;;
		*)   label="$v (proposals)" ;;
	esac
	rows="${rows}| ${label} | ${pct}% | ${passed} | ${smod} / ${sass} |
"
	summary="${summary}${pct}% (${v}) · "
done
summary="WebAssembly spec: $(printf '%s' "$summary" | sed 's/ · $//')"

body=$(printf '| Version | Pass rate | Passed | Skipped (modules / assertions) |\n|---|---|---|---|\n%s' "$rows")
printf '%s\n%s\n' "$summary" "$body" >"$report"
printf '%s\n' "$summary"
