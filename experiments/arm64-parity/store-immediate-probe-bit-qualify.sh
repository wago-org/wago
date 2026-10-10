#!/bin/sh
set -eu
export WAGO_SHARED_SCALAR=0
export WAGO_PARITY_CORPUS=/Users/work/Code/Web/wasm.fyi/corpora/applications
go test -tags wago_codegenstats,wago_regalloccheck,wago_guardpage ./src/core/compiler/backend/railshot/arm64 ./src/core/encoder/arm64 ./src/core/runtime > experiments/arm64-parity/store-immediate-probe-bit-tests.txt 2>&1
go test -tags wago_guardpage -c -o /tmp/parity-store-immediate-probe-bit.test ./experiments/arm64-parity
WAGO_PARITY_CODE_DIR=/tmp/store-immediate-probe-bit /tmp/parity-store-immediate-probe-bit.test -test.run=^$ -test.bench='BenchmarkParity/(search-aho-corasick|graph-dijkstra|compiler-dead-code|compiler-constant-fold|search-bk-tree|geo-polyline-simplify|db-btree)/Exec$' -test.benchtime=1x > experiments/arm64-parity/store-immediate-probe-bit-oracles.txt 2>&1
for image in /tmp/store-immediate-probe-bit/*.bin; do
 cmp "$image" "/tmp/store-immediate-probe/${image##*/}"
done
index=0
for variant in before after after before; do
 binary=/tmp/parity-store-immediate-probe.test
 if [ "$variant" = after ]; then binary=/tmp/parity-store-immediate-probe-bit.test; fi
 "$binary" -test.run=^$ -test.bench='BenchmarkParity/(search-aho-corasick|compiler-dead-code|geo-polyline-simplify|db-btree)/Compile$' -test.benchtime=200ms -test.count=2 > "experiments/arm64-parity/store-immediate-probe-bit-$index-$variant.txt" 2>&1
 index=$((index+1))
done
