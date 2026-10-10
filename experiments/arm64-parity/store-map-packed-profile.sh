#!/bin/sh
set -eu
export WAGO_SHARED_SCALAR=0 WAGO_ARM64_EXPERIMENT_STORE_MAP=1
go build -tags wago_runtime,wago_profile,wago_guardpage -o /tmp/wago-store-map-packed-prof ./cli/wago
/tmp/wago-store-map-packed-prof profile record --module /Users/work/Code/Web/wasm.fyi/corpora/applications/artifacts/vision-components.wasm --export benchmark --args 128 --want 1698848657 --bounds signals --mode prepared --duration 3s --backend samply --samply /opt/homebrew/bin/samply --rate 1000 --include-code --source-maps --out experiments/arm64-parity/profile-components-store-map-packed > experiments/arm64-parity/profile-components-store-map-packed.txt 2>&1
/tmp/wago-store-map-packed-prof profile top experiments/arm64-parity/profile-components-store-map-packed > experiments/arm64-parity/components-store-map-packed-top.txt
/tmp/wago-profile-tools/bin/python experiments/arm64-parity/sampled_assembly.py experiments/arm64-parity/profile-components-store-map-packed > experiments/arm64-parity/components-store-map-packed-sampled-assembly.txt
