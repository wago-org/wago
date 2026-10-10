#!/bin/sh
set -eu
export WAGO_SHARED_SCALAR=0
export WAGO_PARITY_CORPUS=/Users/work/Code/Web/wasm.fyi/corpora/applications
index=0
for variant in after before before after; do
 binary=/tmp/parity-store-map-removed.test
 if [ "$variant" = after ]; then binary=/tmp/parity-store-immediate-probe-bit.test; fi
 "$binary" -test.run=^$ -test.bench='BenchmarkParity/(compiler-dead-code|search-bk-tree|search-aho-corasick|db-btree)/(Compile|Exec)$' -test.benchtime=300ms -test.count=2 > "experiments/arm64-parity/store-immediate-probe-final-$index-$variant.txt" 2>&1
 index=$((index+1))
done
go build -tags wago_runtime,wago_profile,wago_guardpage -o /tmp/wago-store-probe-prof ./cli/wago
/tmp/wago-store-probe-prof profile record --module /Users/work/Code/Web/wasm.fyi/corpora/applications/artifacts/vision-components.wasm --export benchmark --args 128 --want 1698848657 --bounds signals --mode prepared --duration 3s --backend samply --samply /opt/homebrew/bin/samply --rate 1000 --include-code --source-maps --out experiments/arm64-parity/profile-components-store-probe > experiments/arm64-parity/profile-components-store-probe.txt 2>&1
/tmp/wago-profile-tools/bin/python experiments/arm64-parity/sampled_assembly.py experiments/arm64-parity/profile-components-store-probe > experiments/arm64-parity/components-store-probe-sampled-assembly.txt
