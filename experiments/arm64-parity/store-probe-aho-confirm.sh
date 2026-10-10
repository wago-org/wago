#!/bin/sh
set -eu
export WAGO_SHARED_SCALAR=0 WAGO_PARITY_CORPUS=/Users/work/Code/Web/wasm.fyi/corpora/applications
index=0
for variant in before after after before; do
 binary=/tmp/parity-store-map-removed.test
 if [ "$variant" = after ]; then binary=/tmp/parity-store-immediate-probe-bit.test; fi
 "$binary" -test.run=^$ -test.bench='BenchmarkParity/search-aho-corasick/Compile$' -test.benchtime=500ms -test.count=3 > "experiments/arm64-parity/store-probe-aho-confirm-$index-$variant.txt" 2>&1
 index=$((index+1))
done
