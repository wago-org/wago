#!/bin/sh
set -eu
export WAGO_SHARED_SCALAR=0
export WAGO_PARITY_CORPUS=/Users/work/Code/Web/wasm.fyi/corpora/applications
go test -tags wago_codegenstats,wago_regalloccheck,wago_guardpage ./src/core/compiler/backend/railshot/arm64 ./src/core/encoder/arm64 ./src/core/runtime > experiments/arm64-parity/store-immediate-probe-tests.txt 2>&1
go test -tags wago_guardpage -c -o /tmp/parity-store-immediate-probe.test ./experiments/arm64-parity
WAGO_PARITY_CODE_DIR=/tmp/store-immediate-probe /tmp/parity-store-immediate-probe.test -test.run=^$ -test.bench='BenchmarkParity/.*/Exec$' -test.benchtime=1x > experiments/arm64-parity/store-immediate-probe-oracles.txt 2>&1
index=0
for variant in before after after before; do
 binary=/tmp/parity-store-map-removed.test
 if [ "$variant" = after ]; then binary=/tmp/parity-store-immediate-probe.test; fi
 "$binary" -test.run=^$ -test.bench='BenchmarkParity/(compiler-register-allocation|search-bk-tree|db-btree|image-resize)/(Compile|Exec)$' -test.benchtime=100ms -test.count=2 > "experiments/arm64-parity/store-immediate-probe-$index-$variant.txt" 2>&1
 index=$((index+1))
done
