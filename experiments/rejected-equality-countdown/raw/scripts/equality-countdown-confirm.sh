#!/bin/sh
set -eu
export WAGO_SHARED_SCALAR=0 WAGO_ARM64_EXPERIMENT_EQUALITY_COUNTDOWN=0
for phase in exec compile; do
 /tmp/paired-equality-countdown-final -option equality-countdown -workloads compression-rle,image-median,bio-lcs,db-sort-merge-join,bio-kmer-count,stats-bootstrap,vision-dilation,grid-pathfinding,game-spatial-hash -phase "$phase" -rounds 12 -budget 300ms -code-dir /tmp/equality-countdown-confirm-code > "experiments/arm64-parity/equality-countdown-confirm-$phase.jsonl" 2>&1
done
python3 experiments/arm64-parity/summarize_paired.py experiments/arm64-parity/equality-countdown-confirm-exec.jsonl experiments/arm64-parity/equality-countdown-confirm-compile.jsonl > experiments/arm64-parity/equality-countdown-confirm-summary.md
