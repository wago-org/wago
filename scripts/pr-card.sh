#!/usr/bin/env sh
# Compose the wago CI info card posted on PRs: one collapsible <details> section
# per topic (coverage, benchmarks, tests, memory, ...). Each producer drops a
# fragment at $CARD_DIR/<key>.md whose first line is the section summary (the
# visible <summary>) and whose rest is the body. Topics without a fragment render
# as a blank placeholder, so the card's shape is stable as producers come online.
set -eu

dir="${CARD_DIR:-ci-card}"
out="${CARD_FILE:-card.md}"
marker="${CARD_MARKER:-<!-- wago-ci -->}"

# Ordered sections: "<key>|<title>". Add a line here when a new producer lands.
sections='coverage|Coverage
benchmarks|Benchmarks
tests|Tests
memory|Memory'

{
	printf '%s\n' "$marker"
	printf '### CI report\n\n'
	printf '%s\n' "$sections" | while IFS='|' read -r key title; do
		frag="$dir/$key.md"
		if [ -s "$frag" ]; then
			summary=$(head -n 1 "$frag")
			body=$(tail -n +2 "$frag")
		else
			summary="$title — _not reported yet_"
			body="_No data._"
		fi
		printf '<details><summary>%s</summary>\n\n%s\n</details>\n' "$summary" "$body"
	done
} >"$out"
