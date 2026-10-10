#!/bin/sh
set -eu
export WAGO_SHARED_SCALAR=0 WAGO_ARM64_EXPERIMENT_STORE_MAP=1
export WAGO_STORE_MAP_STACK_CASES=/tmp/store-map-stack-cases.json
go test -tags wago_codegenstats,wago_regalloccheck,wago_guardpage ./src/core/compiler/backend/railshot/arm64 -run TestStoreMapStackAddressMachineCases -count=1 > experiments/arm64-parity/store-map-stack-tests.txt 2>&1
/tmp/wago-profile-tools/bin/python experiments/arm64-parity/store_map_stack_emulation.py "$WAGO_STORE_MAP_STACK_CASES" > experiments/arm64-parity/store-map-stack-emulation.txt 2>&1
go test -tags wago_codegenstats,wago_regalloccheck,wago_guardpage ./src/core/compiler/backend/railshot ./src/core/compiler/backend/railshot/arm64 ./src/core/runtime ./src/core/encoder/arm64 ./src/core/compiler/optimization > experiments/arm64-parity/store-map-stack-diagnostics.txt 2>&1
