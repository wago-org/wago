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
bench_isa="${WAGO_BENCH_ISA:-0}"
benchpub_isa_flag=""
if [ "$bench_isa" = 1 ] || [ "$bench_isa" = true ] || [ "$bench_isa" = yes ]; then
	benchpub_isa_flag="-isa"
fi

# WAGO_BENCH_IN: publish a previously captured `go test -bench` output instead of
# re-running the suite. Capture one (once) with:
#   cd bench && go test -run '^$' -bench . -benchmem -count 6 -timeout 0 . | tee run.txt
# then: WAGO_BENCH_IN=run.txt make bench-publish
# Resolve to an absolute path now, before the script cd's into the docs clone.
bench_in="${WAGO_BENCH_IN:-}"
[ -z "$bench_in" ] || case "$bench_in" in /*) : ;; *) bench_in="$(pwd)/$bench_in" ;; esac

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

if [ -n "$bench_in" ]; then
	# Reuse a captured run: skip the suite, and skip WARP collection (it runs the
	# harness) unless a harness was explicitly requested via WAGO_WARP_HARNESS.
	warp="${WAGO_WARP_HARNESS:-}"
	set -- -in "$bench_in"
	printf 'wago: publishing saved run %s (suite not re-run)\n' "$bench_in"
else
	# Build the WARP comparison harness if possible (best-effort; benchpub skips
	# WARP when the binary is absent).
	sh "$root/scripts/build-warp-bench.sh" 2>/dev/null || printf 'wago: WARP harness unavailable; comparison will omit WARP\n'
	warp="${WAGO_WARP_HARNESS:-auto}"
	set -- -benchtime "$benchtime" -count "$count"
	printf 'wago: running benchmark suite (benchtime=%s count=%s)...\n' "$benchtime" "$count"
fi

(cd "$root/bench" && go run ./cmd/benchpub "$@" \
	-warp "$warp" \
	$benchpub_isa_flag \
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
