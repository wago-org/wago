#!/bin/sh
set -eu
export WAGO_SHARED_SCALAR=0 WAGO_ARM64_EXPERIMENT_STORE_MAP=1
export WAGO_PARITY_CORPUS=/Users/work/Code/Web/wasm.fyi/corpora/applications
WAGO_STORE_MAP_GUARD_CASES=/tmp/store-map-trip-guard-cases.json go test -tags wago_codegenstats,wago_regalloccheck,wago_guardpage ./src/core/compiler/backend/railshot/arm64 -run TestStoreMapGuardMachineCases -count=1 > experiments/arm64-parity/store-map-trip-guard-tests.txt 2>&1
/tmp/wago-profile-tools/bin/python experiments/arm64-parity/store_map_guard_emulation.py /tmp/store-map-trip-guard-cases.json > experiments/arm64-parity/store-map-trip-guard-emulation.txt 2>&1
go build -tags wago_runtime,wago_profile,wago_guardpage -o /tmp/wago-store-map-trip-prof ./cli/wago
/tmp/wago-store-map-trip-prof profile record --module /Users/work/Code/Web/wasm.fyi/corpora/applications/artifacts/vision-components.wasm --export benchmark --args 128 --want 1698848657 --bounds signals --mode prepared --duration 3s --backend samply --samply /opt/homebrew/bin/samply --rate 1000 --include-code --source-maps --out experiments/arm64-parity/profile-components-store-map-trip-qualified > experiments/arm64-parity/profile-components-store-map-trip.txt 2>&1
for mode in off on; do
 flag=0
 if [ "$mode" = on ]; then flag=1; fi
 WAGO_ARM64_EXPERIMENT_STORE_MAP=$flag /tmp/parity-store-map-affine.test -test.run=^$ -test.bench='BenchmarkParity/ml-inference/Compile$' -test.benchtime=2s -test.count=1 -test.cpuprofile="/tmp/store-map-trip-compile-$mode.pprof" > "experiments/arm64-parity/store-map-trip-compile-$mode.txt" 2>&1
 go tool pprof -top -nodecount=25 /tmp/parity-store-map-affine.test "/tmp/store-map-trip-compile-$mode.pprof" > "experiments/arm64-parity/store-map-trip-compile-$mode-top.txt" 2>&1
done
