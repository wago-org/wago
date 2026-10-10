#!/bin/sh
set -eu
export WAGO_SHARED_SCALAR=0 WAGO_ARM64_EXPERIMENT_STORE_MAP=1
export WAGO_PARITY_CORPUS=/Users/work/Code/Web/wasm.fyi/corpora/applications
WAGO_STORE_MAP_GUARD_CASES=/tmp/store-map-induction-guard-cases.json go test -tags wago_codegenstats,wago_regalloccheck,wago_guardpage ./src/core/compiler/backend/railshot/arm64 -run TestStoreMap -count=1 > experiments/arm64-parity/store-map-induction-tests.txt 2>&1
/tmp/wago-profile-tools/bin/python experiments/arm64-parity/store_map_guard_emulation.py /tmp/store-map-induction-guard-cases.json > experiments/arm64-parity/store-map-induction-guard-emulation.txt 2>&1
go test -tags wago_guardpage -c -o /tmp/parity-store-map-induction.test ./experiments/arm64-parity
index=0
for variant in before after after before; do
 binary=/tmp/parity-store-map-constant-alias.test
 if [ "$variant" = after ]; then binary=/tmp/parity-store-map-induction.test; fi
 "$binary" -test.run=^$ -test.bench='BenchmarkParity/(ml-inference|vision-components|db-sort-merge-join|stats-bootstrap)/(Compile|Exec)$' -test.benchtime=200ms -test.count=2 > "experiments/arm64-parity/store-map-induction-$index-$variant.txt" 2>&1
 index=$((index+1))
done
