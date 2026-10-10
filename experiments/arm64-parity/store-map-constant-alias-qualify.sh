#!/bin/sh
set -eu
export WAGO_SHARED_SCALAR=0 WAGO_ARM64_EXPERIMENT_STORE_MAP=1
export WAGO_PARITY_CORPUS=/Users/work/Code/Web/wasm.fyi/corpora/applications
go test -tags wago_codegenstats,wago_regalloccheck,wago_guardpage ./src/core/compiler/backend/railshot ./src/core/compiler/backend/railshot/arm64 ./src/core/runtime ./src/core/encoder/arm64 ./src/core/compiler/optimization > experiments/arm64-parity/store-map-constant-alias-tests.txt 2>&1
WAGO_STORE_MAP_GUARD_CASES=/tmp/store-map-constant-alias-guard-cases.json go test -tags wago_codegenstats,wago_regalloccheck,wago_guardpage ./src/core/compiler/backend/railshot/arm64 -run TestStoreMapGuardMachineCases -count=1 > experiments/arm64-parity/store-map-constant-alias-guard-tests.txt 2>&1
/tmp/wago-profile-tools/bin/python experiments/arm64-parity/store_map_guard_emulation.py /tmp/store-map-constant-alias-guard-cases.json > experiments/arm64-parity/store-map-constant-alias-guard-emulation.txt 2>&1
go test -tags wago_guardpage -c -o /tmp/parity-store-map-constant-alias.test ./experiments/arm64-parity
go test -c -o /tmp/parity-store-map-constant-alias-explicit.test ./experiments/arm64-parity
/tmp/parity-store-map-constant-alias.test -test.run=^$ -test.bench='BenchmarkParity/.*/Exec$' -test.benchtime=1x > experiments/arm64-parity/store-map-constant-alias-signal-oracles.txt 2>&1
/tmp/parity-store-map-constant-alias-explicit.test -test.run=^$ -test.bench='BenchmarkParity/.*/Exec$' -test.benchtime=1x > experiments/arm64-parity/store-map-constant-alias-explicit-oracles.txt 2>&1
index=0
for variant in before after after before; do
 binary=/tmp/parity-code-buffer-adopt.test
 if [ "$variant" = after ]; then binary=/tmp/parity-store-map-constant-alias.test; fi
 "$binary" -test.run=^$ -test.bench='BenchmarkParity/(ml-inference|vision-components|image-resize|db-sort-merge-join|stats-bootstrap|ml-max-pooling)/(Compile|Exec)$' -test.benchtime=200ms -test.count=2 > "experiments/arm64-parity/store-map-constant-alias-$index-$variant.txt" 2>&1
 index=$((index+1))
done
