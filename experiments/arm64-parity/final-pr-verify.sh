#!/bin/sh
set -eu
# Run through ~/benchmark-lock.py --wait --owner root-pr-wrap -- sh THIS_FILE.
export WAGO_SHARED_SCALAR=0
export WAGO_PARITY_CACHE=/Users/work/Code/Web/wasm.fyi/.wasmbench/corpus-cache
export WAGO_PARITY_CORPUS=/Users/work/Code/Web/wasm.fyi/corpora/applications
unset WAGO_ARM64_NO_GUARDED_BOOLEAN_XOR WAGO_ARM64_NO_TAKEN_ZERO_BRANCH WAGO_ARM64_NO_INTEGER_SELECT_LOCAL_READ WAGO_ARM64_NO_BOUNDED_BRANCH_INDEX WAGO_ARM64_NO_CSET_LINEAGE
root=experiments/arm64-parity
# The initial full plain run is preserved in final-pr-plain.txt; schema failure
# was repaired before merging main. Keep unrelated local failures visible.
suite_status=0
if go test -p 4 -tags=wago_regalloccheck ./... > "$root/final-pr-regalloc.txt" 2>&1; then
 printf 'full regalloc: PASS\n' > "$root/final-pr-check-status.txt"
else
 printf 'full regalloc: FAIL (see final-pr-regalloc.txt)\n' > "$root/final-pr-check-status.txt"
 suite_status=1
fi
go test . ./src/... ./profile/... ./cli/internal/settings > "$root/final-pr-source-plain.txt" 2>&1
(cd bench && go test -tags=wago_regalloccheck ./...) > "$root/final-pr-bench-tests.txt" 2>&1
go test -tags wago_codegenstats,wago_regalloccheck,wago_guardpage ./src/core/compiler/backend/railshot/arm64 ./src/core/compiler/optimization ./src/core/encoder/arm64 ./src/core/runtime > "$root/final-pr-diagnostics.txt" 2>&1
go test -c -tags wago_guardpage -o /tmp/parity-final-pr-signal.test ./experiments/arm64-parity
go test -c -o /tmp/parity-final-pr-explicit.test ./experiments/arm64-parity
for bounds in signal explicit; do
 WAGO_PARITY_CORE_CODE_DIR=/tmp/final-pr-$bounds-core /tmp/parity-final-pr-$bounds.test -test.run '^TestCachedCoreOracles$' -test.v > "$root/final-pr-$bounds-core.txt" 2>&1
 WAGO_PARITY_CODE_DIR=/tmp/final-pr-$bounds-app /tmp/parity-final-pr-$bounds.test -test.run '^$' -test.bench '^BenchmarkParity/.*/Exec$' -test.benchtime=1x -test.count=1 > "$root/final-pr-$bounds-app.txt" 2>&1
done
python3 "$root/final-pr-native-proof.py"
just test spec v2 > "$root/final-pr-spec-v2.txt" 2>&1
printf 'source plain, bench tests, diagnostics, spec v2 and both-bounds exact images: PASS\n' >> "$root/final-pr-check-status.txt"
cat "$root/final-pr-check-status.txt"
exit "$suite_status"
