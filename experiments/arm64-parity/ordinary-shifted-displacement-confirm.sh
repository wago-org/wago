#!/bin/sh
set -eu
export WAGO_SHARED_SCALAR=0 WAGO_ARM64_EXPERIMENT_ORDINARY_SHIFTED_IMMEDIATE=0
for phase in exec compile; do
 /tmp/paired-ordinary-shifted-displacement -option ordinary-shifted-immediate -workloads files-glob-match,vision-components,document-edit-distance,triangle-raster,video-optical-flow,stencil-wave-equation,compression-lzw -phase "$phase" -rounds 10 -budget 200ms > "experiments/arm64-parity/ordinary-shifted-displacement-confirm-$phase.jsonl" 2>&1
done
python3 experiments/arm64-parity/summarize_paired.py experiments/arm64-parity/ordinary-shifted-displacement-confirm-*.jsonl > experiments/arm64-parity/ordinary-shifted-displacement-confirm-summary.md
