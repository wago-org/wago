#!/usr/bin/env sh
# Run the test suite with cross-package coverage and render a per-package report.
# Backs `make cover` and the CI coverage job. When COVER_BASELINE_REF is set
# (e.g. origin/main) the report gains a "Δ vs main" column by measuring that ref
# in a throwaway worktree. In GitHub Actions the report is appended to
# $GITHUB_STEP_SUMMARY; it is always written to $COVER_REPORT for the PR comment.
set -eu

profile="${COVERPROFILE:-coverage.out}"
report="${COVER_REPORT:-coverage-report.md}"
marker="${COVER_MARKER:-<!-- wago-coverage -->}"
baseline_ref="${COVER_BASELINE_REF:-}"
tab=$(printf '\t')

root=$(git rev-parse --show-toplevel) || {
	printf 'wago: not inside a git repository\n' >&2
	exit 1
}
cd "$root"

# measure <dir> <profile-out>: run coverage for the module rooted at <dir> and
# print "covered<TAB>total<TAB>pkg" per package, plus a final TOTAL row. The
# merged profile repeats each block once per test binary, so dedup by block id.
measure() {
	(cd "$1" && go test -count=1 -covermode=atomic -coverpkg=./... -coverprofile="$2" ./... >/dev/null)
	awk 'NR>1 {
		key=$1; stmts[key]=$2+0; c=$3+0; if (c>max[key]) max[key]=c; seen[key]=1
	}
	END {
		for (k in seen) {
			f=k; sub(/:[0-9]+\.[0-9]+,[0-9]+\.[0-9]+$/, "", f)
			p=f; sub(/\/[^/]+\.go$/, "", p); sub(/^github.com\/wago-org\/wago\/?/, "./", p)
			if (p == "") p = "./"
			tot[p]+=stmts[k]; if (max[k]>0) cov[p]+=stmts[k]; T+=stmts[k]; if (max[k]>0) C+=stmts[k]
		}
		for (p in tot) printf "%d\t%d\t%s\n", cov[p], tot[p], p
		printf "%d\t%d\tTOTAL\n", C, T
	}' "$2"
}

cur=$(mktemp)
measure "$root" "$profile" >"$cur"

base=$(mktemp) # stays empty unless a resolvable baseline is measured
if [ -n "$baseline_ref" ] && git rev-parse --verify -q "$baseline_ref^{commit}" >/dev/null 2>&1; then
	wt=$(mktemp -d)
	git worktree add --detach -q "$wt" "$baseline_ref"
	measure "$wt" "$wt/coverage.out" >"$base" 2>/dev/null || : >"$base"
	git worktree remove --force "$wt" 2>/dev/null || true
fi

# Render: route baseline rows by FILENAME (an empty baseline must not be mistaken
# for the current summary), emit a TOTAL line + pct-keyed package rows.
have_base=0
[ -s "$base" ] && have_base=1
rendered=$(awk -F"$tab" -v basef="$base" -v have_base="$have_base" '
	function pct(c, t) { return t > 0 ? 100.0*c/t : 0 }
	function delta(p, pc,   d) {
		if (!have_base) return "-"
		if (!(p in btot)) return "new"
		d = pc - pct(bcov[p], btot[p])
		if (d > 0.049) return sprintf("+%.1f", d)
		if (d < -0.049) return sprintf("%.1f", d)
		return "—"
	}
	FILENAME == basef { bcov[$3]=$1; btot[$3]=$2; next }
	{
		pc = pct($1, $2); d = delta($3, pc)
		if ($3 == "TOTAL") { printf "TOTAL%s%.1f%s%s%s%d/%d\n", FS, pc, FS, d, FS, $1, $2; next }
		printf "ROW%s%.1f%s%s%s%d/%d%s%s\n", FS, pc, FS, d, FS, $1, $2, FS, $3
	}
' "$base" "$cur")

total_line=$(printf '%s\n' "$rendered" | awk -F"$tab" '$1=="TOTAL"{print}')
total_pct=$(printf '%s' "$total_line" | cut -f2)
total_delta=$(printf '%s' "$total_line" | cut -f3)

# Header: "## Coverage: 68.6% (+0.2% vs main)"
head="## Coverage: ${total_pct}%"
if [ "$have_base" = 1 ] && [ -n "$total_delta" ] && [ "$total_delta" != "—" ]; then
	head="$head (${total_delta}% vs main)"
fi

# Table, sorted by coverage ascending. Δ column only when a baseline was measured.
rows=$(printf '%s\n' "$rendered" | awk -F"$tab" '$1=="ROW"' | sort -t"$tab" -k2,2n)
md=$(
	printf '%s\n%s\n\n' "$marker" "$head"
	if [ "$have_base" = 1 ]; then
		printf '| Coverage | Δ vs main | Statements | Package |\n|---|---|---|---|\n'
		printf '%s\n' "$rows" | while IFS="$tab" read -r _ pc d sc pkg; do
			printf '| %s%% | %s | %s | `%s` |\n' "$pc" "${d:-—}" "$sc" "$pkg"
		done
	else
		printf '| Coverage | Statements | Package |\n|---|---|---|\n'
		printf '%s\n' "$rows" | while IFS="$tab" read -r _ pc d sc pkg; do
			printf '| %s%% | %s | `%s` |\n' "$pc" "$sc" "$pkg"
		done
	fi
)

printf '\nCoverage by package (statement-weighted):\n%s\n' "$md"
printf '%s\n' "$md" >"$report"
if [ -n "${GITHUB_STEP_SUMMARY:-}" ]; then
	printf '%s\n' "$md" >>"$GITHUB_STEP_SUMMARY"
fi

rm -f "$cur" "$base"
