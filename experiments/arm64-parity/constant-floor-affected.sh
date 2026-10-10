#!/bin/sh
set -eu
export WAGO_SHARED_SCALAR=0 WAGO_PARITY_CORPUS=/Users/work/Code/Web/wasm.fyi/corpora/applications
index=0
for variant in after before before after; do
 binary=/tmp/parity-store-immediate-probe-bit.test
 if [ "$variant" = after ]; then binary=/tmp/parity-constant-floor.test; fi
 "$binary" -test.run=^$ -test.bench='BenchmarkParity/(bio-global-alignment|bio-local-alignment|db-btree|vision-distance-transform)/(Compile|Exec)$' -test.benchtime=300ms -test.count=2 > "experiments/arm64-parity/constant-floor-affected-$index-$variant.txt" 2>&1
 index=$((index+1))
done
