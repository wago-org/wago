#!/bin/sh
set -eu
export WAGO_SHARED_SCALAR=0
export WAGO_PARITY_CORPUS=/Users/work/Code/Web/wasm.fyi/corpora/applications
go test -c -o /tmp/parity-store-immediate-probe-explicit.test ./experiments/arm64-parity
/tmp/parity-store-immediate-probe-explicit.test -test.run=^$ -test.bench='BenchmarkParity/.*/Exec$' -test.benchtime=1x > experiments/arm64-parity/store-immediate-probe-explicit-oracles.txt 2>&1
index=0
for variant in after before before after; do
 binary=/tmp/parity-store-map-removed.test
 if [ "$variant" = after ]; then binary=/tmp/parity-store-immediate-probe.test; fi
 "$binary" -test.run=^$ -test.bench='BenchmarkParity/(search-aho-corasick|graph-dijkstra|compiler-dead-code|compiler-constant-fold|search-bk-tree|geo-polyline-simplify|db-btree)/(Compile|Exec)$' -test.benchtime=200ms -test.count=2 > "experiments/arm64-parity/store-immediate-probe-confirm-$index-$variant.txt" 2>&1
 index=$((index+1))
done
