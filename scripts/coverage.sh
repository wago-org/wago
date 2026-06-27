#!/usr/bin/env sh
# Run the test suite with cross-package coverage and print a per-package report.
# Backs `make cover` and the CI coverage job. In GitHub Actions (where
# $GITHUB_STEP_SUMMARY is set) the report is also appended to the run summary.
set -eu

profile="${COVERPROFILE:-coverage.out}"

root=$(git rev-parse --show-toplevel) || {
	printf 'wago: not inside a git repository\n' >&2
	exit 1
}
cd "$root"

# -coverpkg=./... so a test in one package counts toward coverage of the packages
# it actually exercises (the spec/api suites drive the compiler and runtime, which
# otherwise read as untested). atomic mode is safe across the parallel binaries.
go test -count=1 -covermode=atomic -coverpkg=./... -coverprofile="$profile" ./...

total=$(go tool cover -func="$profile" | awk '/^total:/{print $NF}')

# Per-package, statement-weighted. The merged profile repeats each block once per
# test binary, so dedup by block id (take the max hit count) before summing.
report=$(awk 'NR>1 {
	key=$1; stmts[key]=$2+0; c=$3+0; if (c>max[key]) max[key]=c; seen[key]=1
}
END {
	for (k in seen) {
		f=k; sub(/:[0-9]+\.[0-9]+,[0-9]+\.[0-9]+$/, "", f)
		p=f; sub(/\/[^/]+\.go$/, "", p); sub(/^github.com\/wago-org\/wago\/?/, "./", p)
		if (p == "") p = "./"
		tot[p]+=stmts[k]; if (max[k]>0) cov[p]+=stmts[k]
	}
	for (p in tot) printf "%6.1f%%\t%d/%d\t%s\n", 100*cov[p]/tot[p], cov[p], tot[p], p
}' "$profile" | sort -n)

printf '\nCoverage by package (statement-weighted):\n%s\nTOTAL: %s\n' "$report" "$total"

# Build the markdown report once and reuse it for the run summary and the PR
# comment. The leading marker (an invisible HTML comment) lets the PR-comment
# step find and update its own comment instead of posting a new one each run.
tab=$(printf '\t')
md=$(
	printf '%s\n' "${COVER_MARKER:-<!-- wago-coverage -->}"
	printf '## Coverage: %s\n\n' "$total"
	printf '| Coverage | Statements | Package |\n|---|---|---|\n'
	printf '%s\n' "$report" | while IFS="$tab" read -r pct stmts pkg; do
		printf '| %s | %s | `%s` |\n' "$pct" "$stmts" "$pkg"
	done
)

printf '%s\n' "$md" >"${COVER_REPORT:-coverage-report.md}"
if [ -n "${GITHUB_STEP_SUMMARY:-}" ]; then
	printf '%s\n' "$md" >>"$GITHUB_STEP_SUMMARY"
fi
