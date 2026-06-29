#!/usr/bin/env sh
# Upsert a "sticky" CI info-card comment on a pull request: create it once, then
# update the same comment on later runs (keyed by the marker line that pr-card.sh
# writes as the first line of the card). Needs gh authenticated via GH_TOKEN with
# pull-requests: write.
#
# Required env: GITHUB_REPOSITORY (owner/repo), PR_NUMBER.
set -eu

file="${CARD_FILE:-card.md}"
repo="${GITHUB_REPOSITORY:?GITHUB_REPOSITORY not set}"
pr="${PR_NUMBER:?PR_NUMBER not set}"

[ -f "$file" ] || {
	printf 'pr-card-comment: card %s not found (build it first)\n' "$file" >&2
	exit 1
}

marker=$(head -n 1 "$file")
body=$(cat "$file")

# Find an existing comment whose first body line is our marker.
id=$(gh api "repos/$repo/issues/$pr/comments" --paginate \
	--jq '.[] | [.id, (.body | split("\n")[0])] | @tsv' \
	| awk -F '\t' -v m="$marker" '$2 == m { print $1; exit }')

if [ -n "$id" ]; then
	gh api -X PATCH "repos/$repo/issues/comments/$id" -f body="$body" >/dev/null
	printf 'pr-card-comment: updated comment %s\n' "$id"
else
	gh api -X POST "repos/$repo/issues/$pr/comments" -f body="$body" >/dev/null
	printf 'pr-card-comment: created comment\n'
fi
