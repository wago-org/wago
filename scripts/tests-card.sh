#!/usr/bin/env sh
# Emit the "Tests" section fragment for the CI card: passed/failed/skipped counts
# from `go test -json ./...` (subtests included), with deltas vs CARD_BASELINE_REF
# (e.g. origin/main) measured in a throwaway worktree. Fragment format matches the
# other producers: line 1 is the <summary>, the rest is the body.
set -eu

report="${TESTS_REPORT:-ci-card/tests.md}"
baseline_ref="${CARD_BASELINE_REF:-}"

root=$(git rev-parse --show-toplevel) || {
	printf 'wago: not inside a git repository\n' >&2
	exit 1
}
cd "$root"

# count <dir>: run the suite as JSON and print "pass<TAB>fail<TAB>skip" over the
# final per-test events (those carry a "Test" field; package-level events do not).
count() {
	(cd "$1" && go test -count=1 -json ./... 2>/dev/null) | awk -F'"' '
		{ action=""; hastest=0
		  for (i=1; i<NF; i++) { if ($i=="Action") action=$(i+2); if ($i=="Test") hastest=1 }
		  if (hastest) { if (action=="pass") p++; else if (action=="fail") f++; else if (action=="skip") s++ } }
		END { printf "%d\t%d\t%d\n", p+0, f+0, s+0 }'
}

cur=$(count "$root")
cp=$(printf '%s' "$cur" | cut -f1)
cf=$(printf '%s' "$cur" | cut -f2)
cs=$(printf '%s' "$cur" | cut -f3)

have_base=0
if [ -n "$baseline_ref" ] && git rev-parse --verify -q "$baseline_ref^{commit}" >/dev/null 2>&1; then
	wt=$(mktemp -d)
	git worktree add --detach -q "$wt" "$baseline_ref"
	base=$(count "$wt" || printf '0\t0\t0\n')
	git worktree remove --force "$wt" 2>/dev/null || true
	bp=$(printf '%s' "$base" | cut -f1)
	bf=$(printf '%s' "$base" | cut -f2)
	bs=$(printf '%s' "$base" | cut -f3)
	have_base=1
fi

# delta <cur> <base>: signed integer, or "—" when unchanged.
delta() {
	d=$(($1 - $2))
	if [ "$d" -gt 0 ]; then printf '+%d' "$d"
	elif [ "$d" -lt 0 ]; then printf '%d' "$d"
	else printf '—'; fi
}

summary="Tests: ${cp} passed"
[ "$cf" -gt 0 ] && summary="$summary, ${cf} failed"
[ "$cs" -gt 0 ] && summary="$summary, ${cs} skipped"
if [ "$have_base" = 1 ]; then
	dt=$(delta "$((cp + cf + cs))" "$((bp + bf + bs))")
	[ "$dt" != "—" ] && summary="$summary (${dt})"
fi

if [ "$have_base" = 1 ]; then
	body=$(printf '| Result | Count | Δ vs main |\n|---|---|---|\n| Passed | %s | %s |\n| Failed | %s | %s |\n| Skipped | %s | %s |\n' \
		"$cp" "$(delta "$cp" "$bp")" "$cf" "$(delta "$cf" "$bf")" "$cs" "$(delta "$cs" "$bs")")
else
	body=$(printf '| Result | Count |\n|---|---|\n| Passed | %s |\n| Failed | %s |\n| Skipped | %s |\n' "$cp" "$cf" "$cs")
fi

printf '%s\n%s\n' "$summary" "$body" >"$report"
printf '%s\n' "$summary"
