#!/bin/sh
set -eu
export WAGO_SHARED_SCALAR=0 WAGO_ARM64_EXPERIMENT_ORDINARY_SHIFTED_IMMEDIATE=0
cache=/Users/work/Code/Web/wasm.fyi/.wasmbench/corpus-cache
for phase in exec compile; do
 /tmp/paired-ordinary-shifted-immediate -option ordinary-shifted-immediate -workloads files-glob-match,audio-adpcm,video-dct,compiler-register-allocation,vision-components,grid-pathfinding,geo-polygon-clipping,files-merkle-tree,search-bk-tree,compression-lzw,audio-fir,compiler-constant-fold -phase "$phase" -rounds 6 -budget 150ms > "experiments/arm64-parity/ordinary-shifted-immediate-focused-$phase.jsonl" 2>&1
 /tmp/paired-ordinary-shifted-immediate -core-"$phase" -phase "$phase" -corpus "$cache" -option ordinary-shifted-immediate -workloads modular-arithmetic,compile-match -rounds 6 -budget 150ms > "experiments/arm64-parity/ordinary-shifted-immediate-focused-core-$phase.jsonl" 2>&1
done
python3 experiments/arm64-parity/summarize_paired.py experiments/arm64-parity/ordinary-shifted-immediate-focused-*.jsonl > experiments/arm64-parity/ordinary-shifted-immediate-focused-summary.md
