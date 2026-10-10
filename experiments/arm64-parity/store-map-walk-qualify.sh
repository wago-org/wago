#!/bin/sh
set -eu
export WAGO_SHARED_SCALAR=0 WAGO_ARM64_EXPERIMENT_STORE_MAP=1
export WAGO_PARITY_CORPUS=/Users/work/Code/Web/wasm.fyi/corpora/applications
go test -overlay /tmp/store-map-walk-source/overlay.json -tags wago_codegenstats,wago_regalloccheck,wago_guardpage ./src/core/compiler/backend/railshot ./src/core/compiler/backend/railshot/arm64 ./src/core/runtime ./src/core/encoder/arm64 ./src/core/compiler/optimization > experiments/arm64-parity/store-map-walk-tests.txt 2>&1
WAGO_STORE_MAP_GUARD_CASES=/tmp/store-map-walk-guard-cases.json go test -overlay /tmp/store-map-walk-source/overlay.json -tags wago_codegenstats,wago_regalloccheck,wago_guardpage ./src/core/compiler/backend/railshot/arm64 -run TestStoreMapGuardMachineCases -count=1 > experiments/arm64-parity/store-map-walk-guard-tests.txt 2>&1
/tmp/wago-profile-tools/bin/python experiments/arm64-parity/store_map_guard_emulation.py /tmp/store-map-walk-guard-cases.json > experiments/arm64-parity/store-map-walk-guard-emulation.txt 2>&1
go test -overlay /tmp/store-map-walk-source/overlay.json -tags wago_guardpage -c -o /tmp/parity-store-map-walk.test ./experiments/arm64-parity
WAGO_PARITY_CODE_DIR=/tmp/store-map-walk-before /tmp/parity-store-map-affine.test -test.run=^$ -test.bench='BenchmarkParity/(ml-inference|vision-components|image-resize|db-sort-merge-join)/Exec$' -test.benchtime=1x > experiments/arm64-parity/store-map-walk-before-oracles.txt 2>&1
WAGO_PARITY_CODE_DIR=/tmp/store-map-walk-after /tmp/parity-store-map-walk.test -test.run=^$ -test.bench='BenchmarkParity/(ml-inference|vision-components|image-resize|db-sort-merge-join)/Exec$' -test.benchtime=1x > experiments/arm64-parity/store-map-walk-after-oracles.txt 2>&1
for image in ml-inference vision-components image-resize db-sort-merge-join; do
 cmp "/tmp/store-map-walk-before/$image.bin" "/tmp/store-map-walk-after/$image.bin"
done
index=0
for variant in before after after before; do
 binary=/tmp/parity-store-map-affine.test
 if [ "$variant" = after ]; then binary=/tmp/parity-store-map-walk.test; fi
 "$binary" -test.run=^$ -test.bench='BenchmarkParity/(ml-inference|vision-components|image-resize|db-sort-merge-join)/Compile$' -test.benchtime=200ms -test.count=2 > "experiments/arm64-parity/store-map-walk-$index-$variant.txt" 2>&1
 index=$((index+1))
done
