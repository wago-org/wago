#!/usr/bin/env sh
# Regenerate the benchmark charts and publish them to the wago-org/docs repo,
# which the root README.md embeds via raw.githubusercontent.com URLs (json-as
# style). Charts are generated here, locally, never in CI: shared CI runners
# produce noisy benchmark numbers, so the charts must come from a stable machine.
set -eu

docs_remote="${WAGO_DOCS_REMOTE:-git@github.com:wago-org/docs.git}"
docs_branch="main"
dest="charts"

root=$(git rev-parse --show-toplevel) || {
	printf 'wago: not inside a git repository\n' >&2
	exit 1
}

out=$(mktemp -d)
clone=$(mktemp -d)
cleanup() { rm -rf "$out" "$clone"; }
trap cleanup EXIT

printf 'wago: generating charts (running benchmarks)...\n'
(cd "$root/bench" && go run ./chart -out "$out")

printf 'wago: cloning %s...\n' "$docs_remote"
git clone -q --depth 1 --branch "$docs_branch" "$docs_remote" "$clone"

mkdir -p "$clone/$dest"
cp "$out"/*.svg "$clone/$dest/"

cd "$clone"
git add -A
if git diff --cached --quiet; then
	printf 'wago: charts unchanged; nothing to publish\n'
	exit 0
fi
git commit -qs -m "charts: regenerate from wago@$(git -C "$root" rev-parse --short HEAD)"
git push -q origin "$docs_branch"
printf 'wago: published %s/*.svg to wago-org/docs (%s)\n' "$dest" "$docs_branch"
