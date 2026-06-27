#!/usr/bin/env sh
# Run the wago benchmark suite, append the results to the rolling history kept in
# the wago-org/docs repo, regenerate the charts, and publish — the time-series
# companion to publish-charts.sh. Like that script, run this on a stable machine
# (never CI): shared runners make benchmark numbers noisy and pollute the trend.
#
# Artifacts land in docs/bench/: bench.json (latest run), history.json (all runs,
# sorted by version), and charts/{latency,trend}-<stage>.svg.
set -eu

docs_remote="${WAGO_DOCS_REMOTE:-git@github.com:wago-org/docs.git}"
docs_branch="main"
benchtime="${WAGO_BENCHTIME:-1s}"
count="${WAGO_BENCH_COUNT:-6}"

root=$(git rev-parse --show-toplevel) || {
	printf 'wago: not inside a git repository\n' >&2
	exit 1
}

clone=$(mktemp -d)
cleanup() { rm -rf "$clone"; }
trap cleanup EXIT

printf 'wago: cloning %s...\n' "$docs_remote"
git clone -q --depth 1 --branch "$docs_branch" "$docs_remote" "$clone"
mkdir -p "$clone/bench/charts"

printf 'wago: running benchmark suite (benchtime=%s count=%s)...\n' "$benchtime" "$count"
(cd "$root/bench" && go run ./cmd/benchpub \
	-benchtime "$benchtime" -count "$count" \
	-history "$clone/bench/history.json" \
	-out "$clone/bench")

cd "$clone"
git add -A
if git diff --cached --quiet; then
	printf 'wago: bench results unchanged; nothing to publish\n'
	exit 0
fi
git commit -qs -m "bench: $(git -C "$root" describe --tags --always --dirty)"
git push -q origin "$docs_branch"
printf 'wago: published bench/ to wago-org/docs (%s)\n' "$docs_branch"
