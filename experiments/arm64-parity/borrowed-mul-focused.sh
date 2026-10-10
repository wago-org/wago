#!/bin/sh
set -eu
export WAGO_SHARED_SCALAR=0 WAGO_ARM64_EXPERIMENT_BORROWED_MUL=0
export WAGO_PARITY_CACHE=/Users/work/Code/Web/wasm.fyi/.wasmbench/corpus-cache
go build -tags wago_guardpage -o /tmp/paired-borrowed-mul ./experiments/arm64-parity/paired
for phase in exec compile; do
 /tmp/paired-borrowed-mul -option borrowed-mul -workloads geo-point-in-polygon,stats-gini,video-dct,video-optical-flow,mesh-skinning,search-aho-corasick -phase "$phase" -rounds 8 -budget 200ms > "experiments/arm64-parity/borrowed-mul-paired-$phase.jsonl" 2>&1
 /tmp/paired-borrowed-mul -core-"$phase" -corpus "$WAGO_PARITY_CACHE" -option borrowed-mul -workloads modular-arithmetic -phase "$phase" -rounds 8 -budget 200ms > "experiments/arm64-parity/borrowed-mul-core-paired-$phase.jsonl" 2>&1
done
