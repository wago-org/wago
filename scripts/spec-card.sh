#!/usr/bin/env sh
# Emit the "WebAssembly spec" section fragment for the CI card: per-version
# pass/skip counts from TestSpecSuiteExec (1.0 MVP core + the 2.0/3.0 proposal
# tests wago attempts). Fragment format matches the other producers: line 1 is the
# <summary>, the rest is the body. Degrades to a placeholder when wast2json or the
# tests/spec submodule is unavailable.
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

suite="$root/tests/spec"
rows=""
summary=""
for v in 1.0 2.0 3.0; do
	line=$(WAGO_SPECTEST_DIR="$suite" WAGO_SPEC_VERSION="$v" \
		go test -count=1 -run TestSpecSuiteExec -v ./src/wago/ 2>/dev/null \
		| grep -oE "TOTAL\[$v\]: assertions passed=[0-9]+ \| skipped modules=[0-9]+ skipped assertions=[0-9]+" || true)
	passed=$(printf '%s' "$line" | sed -nE 's/.*passed=([0-9]+).*/\1/p'); passed=${passed:-0}
	smod=$(printf '%s' "$line" | sed -nE 's/.*modules=([0-9]+).*/\1/p'); smod=${smod:-0}
	sass=$(printf '%s' "$line" | sed -nE 's/.*assertions=([0-9]+)$/\1/p'); sass=${sass:-0}
	case "$v" in
		1.0) label="1.0 (MVP core)" ;;
		*)   label="$v (proposals)" ;;
	esac
	rows="${rows}| ${label} | ${passed} | ${smod} / ${sass} |
"
	summary="${summary}${v} ${passed} · "
done
summary="WebAssembly spec: $(printf '%s' "$summary" | sed 's/ · $//')"

body=$(printf '| Version | Passed | Skipped (modules / assertions) |\n|---|---|---|\n%s' "$rows")
printf '%s\n%s\n' "$summary" "$body" >"$report"
printf '%s\n' "$summary"
