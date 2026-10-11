#!/bin/sh
set -eu
export WAGO_SHARED_SCALAR=0 WAGO_ARM64_EXPERIMENT_EQUALITY_COUNTDOWN=0 WAGO_ARM64_EQUALITY_COUNTDOWN_DEAD_ONLY=1
go test -tags wago_codegenstats,wago_regalloccheck,wago_guardpage ./src/core/compiler/backend/railshot/arm64 -run '^TestEqualityCountdown' -v > experiments/arm64-parity/equality-countdown-dead-only-tests.txt 2>&1
go build -tags wago_guardpage -o /tmp/paired-equality-countdown-dead-only ./experiments/arm64-parity/paired
for phase in exec compile; do
 /tmp/paired-equality-countdown-dead-only -option equality-countdown -workloads compression-rle,bio-lcs,grid-pathfinding,vision-dilation,game-spatial-hash,db-sort-merge-join,image-median,bio-kmer-count -phase "$phase" -rounds 8 -budget 200ms -code-dir /tmp/equality-countdown-dead-only-code > "experiments/arm64-parity/equality-countdown-dead-only-$phase.jsonl" 2>&1
done
python3 experiments/arm64-parity/summarize_paired.py experiments/arm64-parity/equality-countdown-dead-only-exec.jsonl experiments/arm64-parity/equality-countdown-dead-only-compile.jsonl > experiments/arm64-parity/equality-countdown-dead-only-summary.md
