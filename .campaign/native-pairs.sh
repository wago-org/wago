#!/usr/bin/env bash
set -euo pipefail
# Usage: native-pairs.sh REPO BASE HEAD OUT CAMPAIGN
# CAMPAIGN contains run.py and locks/{gate,build0,build1}; the manager supplies it.
repo=$(realpath "$1")
base=$2
head=$3
out=$(realpath -m "$4")
campaign=$(realpath "$5")
mkdir -p "$out"
case $(uname -m) in aarch64|arm64) ;; *) echo 'Native ARM64 required' >&2; exit 2;; esac
export GOMAXPROCS=4
{
 date -u
 uname -a
 go version
 go env GOOS GOARCH GOMAXPROCS
 lscpu
 printf 'baseline=%s\ncandidate=%s\n' "$base" "$head"
} > "$out/environment.txt"
base_added=0
head_added=0
cleanup() {
 if (( base_added )); then git -C "$repo" worktree remove --force "$out/base-tree"; fi
 if (( head_added )); then git -C "$repo" worktree remove --force "$out/head-tree"; fi
}
trap cleanup EXIT
git -C "$repo" worktree add --detach "$out/base-tree" "$base"
base_added=1
git -C "$repo" worktree add --detach "$out/head-tree" "$head"
head_added=1
for file in src/wago/instruction_test.go src/wago/instruction_recipe_native_test.go src/wago/instruction_recipe_bench_test.go; do
 git -C "$repo" show "$head:$file" > "$out/base-tree/$file"
done
git -C "$out/base-tree" diff --binary > "$out/baseline-test-overlay.diff"
for stage in base head; do
 (
  cd "$out/$stage-tree"
  python3 "$campaign/run.py" build go test -p 4 -c -o "$out/$stage.test" ./src/wago
 )
done
(
 cd "$out/head-tree"
 python3 "$campaign/run.py" build go test -p 4 -c -tags=wago_regalloccheck -o "$out/head-debug.test" ./src/wago
)
sha256sum "$out/base.test" "$out/head.test" "$out/head-debug.test" > "$out/binary-sha256.txt"
python3 "$campaign/run.py" build env WAGO_RECIPE_CODE_DIR="$out/base-code" "$out/base.test" -test.run '^(TestInstructionDoesNotReplaceDistinctImport|TestCustomInstructionChecksumLoop|TestCustomInstructionWideValuesAndMultiResults|TestCustomInstructionSignatureAndHandlerErrors)$' -test.count=1 -test.v > "$out/base-oracle.txt" 2>&1
for binary in head head-debug; do
 python3 "$campaign/run.py" build env WAGO_RECIPE_CODE_DIR="$out/$binary-code" "$out/$binary.test" -test.run 'TestCustomInstruction|TestInstructionDoesNotReplaceDistinctImport|TestCustomType|TestPluginGCHostImport' -test.count=1 -test.v > "$out/$binary-correctness.txt" 2>&1
done
# Each executable is prebuilt. No build runs during this session.
# The first process in each pair alternates. Raw samples retain the pair id.
python3 "$campaign/run.py" bench bash -s -- "$out" <<'BENCH'
set -euo pipefail
out=$1
for pair in 1 2 3 4 5 6 7; do
 order='base head'
 if (( pair % 2 == 0 )); then order='head base'; fi
 for stage in $order; do
  "$out/$stage.test" -test.run '^$' -test.bench '^(BenchmarkInstructionRecipe(Invoke|Compile)|BenchmarkInvokeHostFuncDirect|BenchmarkHostRoundtripLoop)$' -test.benchmem -test.benchtime=250ms -test.count=1 -test.cpu=4 > "$out/pair-$pair-$stage.txt" 2>&1
 done
done
BENCH
