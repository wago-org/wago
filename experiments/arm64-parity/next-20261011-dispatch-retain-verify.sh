#!/bin/sh
set -eu
export WAGO_SHARED_SCALAR=0
export WAGO_PARITY_CACHE=/Users/work/Code/Web/wasm.fyi/.wasmbench/corpus-cache
export WAGO_PARITY_CORPUS=/Users/work/Code/Web/wasm.fyi/corpora/applications
unset WAGO_ARM64_NO_GUARDED_BOOLEAN_XOR WAGO_ARM64_NO_TAKEN_ZERO_BRANCH WAGO_ARM64_NO_INTEGER_SELECT_LOCAL_READ WAGO_ARM64_NO_BOUNDED_BRANCH_INDEX
root=experiments/arm64-parity
go test ./src/core/compiler/backend/railshot/arm64 ./src/core/compiler/optimization ./src/core/encoder/arm64 > "$root/next-20261011-dispatch-retain-plain.txt" 2>&1
go test -tags wago_codegenstats,wago_regalloccheck,wago_guardpage ./src/core/compiler/backend/railshot/arm64 ./src/core/compiler/optimization ./src/core/encoder/arm64 ./src/core/runtime > "$root/next-20261011-dispatch-retain-diagnostics.txt" 2>&1
go test -c -tags wago_guardpage -o /tmp/parity-next-20261011-dispatch-retained-signal.test ./experiments/arm64-parity
go test -c -o /tmp/parity-next-20261011-dispatch-retained-explicit.test ./experiments/arm64-parity
go build -tags wago_guardpage -o /tmp/paired-next-20261011-dispatch-retained ./experiments/arm64-parity/paired
for state in on off; do
 if [ "$state" = off ]; then export WAGO_ARM64_NO_GUARDED_BOOLEAN_XOR=1 WAGO_ARM64_NO_TAKEN_ZERO_BRANCH=1 WAGO_ARM64_NO_INTEGER_SELECT_LOCAL_READ=1 WAGO_ARM64_NO_BOUNDED_BRANCH_INDEX=1; fi
 WAGO_PARITY_CORE_CODE_DIR=/tmp/next-20261011-dispatch-$state-core /tmp/parity-next-20261011-dispatch-retained-signal.test -test.run '^TestCachedCoreOracles$' -test.v > "$root/next-20261011-dispatch-$state-signal-core.txt" 2>&1
 WAGO_PARITY_CODE_DIR=/tmp/next-20261011-dispatch-$state-app /tmp/parity-next-20261011-dispatch-retained-signal.test -test.run '^$' -test.bench '^BenchmarkParity/.*/Exec$' -test.benchtime=1x -test.count=1 > "$root/next-20261011-dispatch-$state-signal-app.txt" 2>&1
done
unset WAGO_ARM64_NO_GUARDED_BOOLEAN_XOR WAGO_ARM64_NO_TAKEN_ZERO_BRANCH WAGO_ARM64_NO_INTEGER_SELECT_LOCAL_READ WAGO_ARM64_NO_BOUNDED_BRANCH_INDEX
/tmp/parity-next-20261011-dispatch-retained-explicit.test -test.run '^TestCachedCoreOracles$' -test.v > "$root/next-20261011-dispatch-on-explicit-core.txt" 2>&1
/tmp/parity-next-20261011-dispatch-retained-explicit.test -test.run '^$' -test.bench '^BenchmarkParity/.*/Exec$' -test.benchtime=1x -test.count=1 > "$root/next-20261011-dispatch-on-explicit-app.txt" 2>&1
python3 experiments/arm64-parity/next-20261011-dispatch-native-proof.py
