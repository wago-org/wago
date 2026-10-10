#!/bin/sh
set -eu
export WAGO_SHARED_SCALAR=0 WAGO_ARM64_EXPERIMENT_BORROWED_MUL=0 WAGO_ARM64_EXPERIMENT_BORROWED_MUL_LAYOUT=1
export WAGO_PARITY_CACHE=/Users/work/Code/Web/wasm.fyi/.wasmbench/corpus-cache
go build -tags wago_guardpage -o /tmp/paired-borrowed-mul-layout ./experiments/arm64-parity/paired
for phase in exec compile; do
 /tmp/paired-borrowed-mul-layout -option borrowed-mul -workloads geo-point-in-polygon,video-optical-flow,mesh-skinning -phase "$phase" -rounds 12 -budget 300ms > "experiments/arm64-parity/borrowed-mul-layout-paired-$phase.jsonl" 2>&1
 /tmp/paired-borrowed-mul-layout -core-"$phase" -corpus "$WAGO_PARITY_CACHE" -option borrowed-mul -workloads modular-arithmetic -phase "$phase" -rounds 12 -budget 300ms > "experiments/arm64-parity/borrowed-mul-layout-core-paired-$phase.jsonl" 2>&1
done
