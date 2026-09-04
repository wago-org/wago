#!/usr/bin/env sh
# Run the public verification gates plus focused wago_gcstats collector coverage
# and render a compact per-package report. Backs `make cover` and the CI coverage
# job. When
# COVER_BASELINE_REF is set (e.g. origin/main) the report gains a "Δ vs main"
# column by measuring that ref in a throwaway worktree. In GitHub Actions the
# report is appended to $GITHUB_STEP_SUMMARY; it is always written to
# $COVER_REPORT for the PR comment.
set -eu

profile="${COVERPROFILE:-coverage.out}"
report="${COVER_REPORT:-coverage-report.md}"
# Shared across card producers; COVER_BASELINE_REF kept for `make cover` alone.
baseline_ref="${COVER_BASELINE_REF:-${CARD_BASELINE_REF:-}}"
tab=$(printf '\t')

root=$(git rev-parse --show-toplevel) || {
	printf 'wago: not inside a git repository\n' >&2
	exit 1
}
cd "$root"

# measure <dir> <profile-out>: run normal, wago_gcstats collector, guard-page,
# spec1, spec2, and SIMD coverage for the module rooted at <dir>, merge by source
# block, and print
# "covered<TAB>total<TAB>pkg" per package, plus a final TOTAL row.
measure() {
	dir=$1
	out=$2
	profiles=$(mktemp -d)
	trap 'rm -rf "$profiles"' EXIT HUP INT TERM

	command -v wast2json >/dev/null 2>&1 || {
		printf 'coverage: wast2json (wabt) not on PATH\n' >&2
		exit 1
	}
	[ -f "$dir/tests/spec/i32.wast" ] ||
		git -C "$dir" submodule update --init tests/spec >/dev/null
	[ -f "$dir/tests/spec-v2/test/core/i32.wast" ] ||
		git -C "$dir" submodule update --init tests/spec-v2 >/dev/null

	(cd "$dir" && go test -count=1 -covermode=atomic -coverpkg=./... \
		-coverprofile="$profiles/normal.out" ./... >/dev/null)
	(cd "$dir" && go test -count=1 -tags wago_gcstats -covermode=atomic \
		-coverpkg=./src/core/runtime/gc/... -coverprofile="$profiles/gcstats.out" \
		./src/core/runtime/gc/... >/dev/null)
	(cd "$dir" && go test -count=1 -tags wago_guardpage -covermode=atomic \
		-coverpkg=./... -coverprofile="$profiles/guard-root.out" ./src/wago/ >/dev/null)
	(cd "$dir/bench" && go test -count=1 -tags wago_guardpage \
		-run 'TestCorpusDifferential|TestJsonAsGuardCorrect' -covermode=atomic \
		-coverpkg=github.com/wago-org/wago/... \
		-coverprofile="$profiles/guard-bench.out" . >/dev/null)
	(cd "$dir" && WAGO_SPECTEST_DIR="$dir/tests/spec" WAGO_SPEC_VERSION=1.0 \
		go test -count=1 -run TestSpecSuiteExec -covermode=atomic -coverpkg=./... \
		-coverprofile="$profiles/spec1.out" ./src/wago/ >/dev/null)
	(cd "$dir" && go test -count=1 -run '^TestCoreV2Validation$' \
		-covermode=atomic -coverpkg=./... -coverprofile="$profiles/spec2-validation.out" \
		./src/core/compiler/wasm/ >/dev/null)
	(cd "$dir" && WAGO_SPECTEST_DIR="$dir/tests/spec-v2" WAGO_SPEC_VERSION=2.0 \
		go test -count=1 -run '^TestCoreV2SpecExecution$' \
		-covermode=atomic -coverpkg=./... -coverprofile="$profiles/spec2-execution.out" \
		./src/wago/ >/dev/null)
	(cd "$dir" && WAGO_SPECTEST_DIR="$dir/tests/spec" WAGO_SPEC_VERSION=simd \
		go test -count=1 -run TestSpecSuiteExec -covermode=atomic -coverpkg=./... \
		-coverprofile="$profiles/simd.out" ./src/wago/ >/dev/null)

	awk '
		FNR == 1 { next }
		{
			key=$1; stmts[key]=$2+0; count=$3+0
			if (count > max[key]) max[key]=count
		}
		END {
			print "mode: atomic"
			for (key in stmts) printf "%s %d %d\n", key, stmts[key], max[key]
		}
	' "$profiles"/*.out >"$out"

	awk 'NR>1 {
		key=$1; stmts[key]=$2+0; c=$3+0; if (c>max[key]) max[key]=c; seen[key]=1
	}
	END {
		for (k in seen) {
			f=k; sub(/:[0-9]+\.[0-9]+,[0-9]+\.[0-9]+$/, "", f)
			p=f; sub("/[^/]+\\.go$", "", p); sub(/^github.com\/wago-org\/wago\/?/, "./", p)
			if (p == "") p = "./"
			tot[p]+=stmts[k]; if (max[k]>0) cov[p]+=stmts[k]; T+=stmts[k]; if (max[k]>0) C+=stmts[k]
		}
		for (p in tot) printf "%d\t%d\t%s\n", cov[p], tot[p], p
		printf "%d\t%d\tTOTAL\n", C, T
	}' "$out"

	rm -rf "$profiles"
	trap - EXIT HUP INT TERM
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
# for the current summary). Emit a TOTAL line + pct-keyed rows with short package
# names; the delta is computed against the full path before shortening.
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
	function short(p) {
		sub(/^\.\//, "", p); if (p == "") return "(root)"
		sub(/^src\/core\/compiler\//, "", p); sub(/^src\/core\//, "", p)
		sub(/^src\//, "", p); sub(/^internal\//, "", p); sub(/^testutil\//, "", p)
		return p
	}
	FILENAME == basef { bcov[$3]=$1; btot[$3]=$2; next }
	{
		pc = pct($1, $2); d = delta($3, pc)
		if ($3 == "TOTAL") { printf "TOTAL%s%.1f%s%s\n", FS, pc, FS, d; next }
		printf "ROW%s%.1f%s%s%s%s\n", FS, pc, FS, d, FS, short($3)
	}
' "$base" "$cur")

total_line=$(printf '%s\n' "$rendered" | awk -F"$tab" '$1=="TOTAL"{print}')
total_pct=$(printf '%s' "$total_line" | cut -f2)
total_delta=$(printf '%s' "$total_line" | cut -f3)
rows=$(printf '%s\n' "$rendered" | awk -F"$tab" '$1=="ROW"' | sort -t"$tab" -k2,2n)
n=$(printf '%s\n' "$rows" | grep -c .)

# Emit a CI-card *section fragment*: line 1 is the summary (the collapsible's
# visible title), the rest is the section body. The card composer (pr-card.sh)
# wraps it in <details>. Summary: "Coverage: 68.8%" + total delta when measured.
summary="Coverage: ${total_pct}%"
if [ "$have_base" = 1 ]; then
	case "$total_delta" in
	"—" | "") summary="$summary (—)" ;;
	*) summary="$summary ($total_delta%)" ;;
	esac
fi

if [ "$have_base" = 1 ]; then
	body=$(
		printf '| Cov | Δ | Package |\n|---|---|---|\n'
		printf '%s\n' "$rows" | while IFS="$tab" read -r _ pc d pkg; do
			printf '| %s%% | %s | `%s` |\n' "$pc" "$d" "$pkg"
		done
	)
else
	body=$(
		printf '| Cov | Package |\n|---|---|\n'
		printf '%s\n' "$rows" | while IFS="$tab" read -r _ pc d pkg; do
			printf '| %s%% | `%s` |\n' "$pc" "$pkg"
		done
	)
fi

printf '%s\n%s\n' "$summary" "$body" >"$report"
printf '\n%s\n' "$summary" # local stdout
if [ -n "${GITHUB_STEP_SUMMARY:-}" ]; then
	printf '### %s\n\n%s\n' "$summary" "$body" >>"$GITHUB_STEP_SUMMARY"
fi

rm -f "$cur" "$base"
