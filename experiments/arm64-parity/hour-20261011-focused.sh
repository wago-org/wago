#!/bin/sh
set -eu
export WAGO_SHARED_SCALAR=0
options=regional-memory-read,dominated-indexed-key,bounded-signed-quotient,wide-reuse-pins,ordinary-shifted-immediate
workloads=audio-adpcm,video-dct,files-glob-match,compiler-register-allocation,vision-components,graphics-reed-solomon,image-median,document-edit-distance,triangle-raster,video-optical-flow,compression-lzw,geo-polygon-clipping,stencil-wave-equation,ml-knn,language-register-vm,vision-dilation
for phase in exec compile; do
 /tmp/paired-hour-20261011 -option "$options" -workloads "$workloads" -phase "$phase" -rounds 8 -budget 150ms -code-dir /tmp/hour-20261011-focused-images > "experiments/arm64-parity/hour-20261011-focused-$phase.jsonl" 2>&1
 /tmp/paired-hour-20261011 -core-"$phase" -phase "$phase" -corpus /Users/work/Code/Web/wasm.fyi/.wasmbench/corpus-cache -option "$options" -workloads decimal-parse,complex-roundtrip,modular-arithmetic -rounds 6 -budget 150ms > "experiments/arm64-parity/hour-20261011-focused-core-$phase.jsonl" 2>&1
done
/tmp/paired-hour-20261011 -core-exec -phase exec -vector-length 16384 -corpus /Users/work/Code/Web/wasm.fyi/.wasmbench/corpus-cache -option "$options" -workloads hash -rounds 6 -budget 150ms > experiments/arm64-parity/hour-20261011-focused-vector.jsonl 2>&1
python3 experiments/arm64-parity/summarize_paired.py experiments/arm64-parity/hour-20261011-focused-*.jsonl > experiments/arm64-parity/hour-20261011-focused-summary.md
