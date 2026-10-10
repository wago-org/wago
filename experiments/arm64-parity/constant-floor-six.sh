#!/bin/sh
set -eu
export WAGO_SHARED_SCALAR=0 WAGO_PARITY_CORPUS=/Users/work/Code/Web/wasm.fyi/corpora/applications
go test -tags wago_codegenstats,wago_regalloccheck,wago_guardpage ./src/core/compiler/backend/railshot/arm64 > experiments/arm64-parity/constant-floor-six-tests.txt 2>&1
go test -tags wago_guardpage -c -o /tmp/parity-constant-floor-six.test ./experiments/arm64-parity
WAGO_PARITY_CODE_DIR=/tmp/constant-floor-six /tmp/parity-constant-floor-six.test -test.run=^$ -test.bench='BenchmarkParity/.*/Exec$' -test.benchtime=1x > experiments/arm64-parity/constant-floor-six-oracles.txt 2>&1
index=0
for variant in after before before after; do
 binary=/tmp/parity-store-immediate-probe-bit.test
 if [ "$variant" = after ]; then binary=/tmp/parity-constant-floor-six.test; fi
 "$binary" -test.run=^$ -test.bench='BenchmarkParity/(bio-global-alignment|bio-local-alignment|db-btree|vision-distance-transform)/(Compile|Exec)$' -test.benchtime=300ms -test.count=2 > "experiments/arm64-parity/constant-floor-six-$index-$variant.txt" 2>&1
 index=$((index+1))
done
