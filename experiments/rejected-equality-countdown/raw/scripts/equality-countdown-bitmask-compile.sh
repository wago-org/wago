#!/bin/sh
set -eu
export WAGO_SHARED_SCALAR=0 WAGO_ARM64_EXPERIMENT_EQUALITY_COUNTDOWN=0
go test -tags wago_codegenstats,wago_regalloccheck,wago_guardpage ./src/core/compiler/backend/railshot/arm64 -run '^TestEqualityCountdown' -v > experiments/arm64-parity/equality-countdown-bitmask-tests.txt 2>&1
go build -tags wago_guardpage -o /tmp/paired-equality-countdown-bitmask ./experiments/arm64-parity/paired
/tmp/paired-equality-countdown-bitmask -option equality-countdown -workloads compression-rle,image-median,bio-lcs,db-sort-merge-join,bio-kmer-count,vision-dilation,grid-pathfinding,files-merkle-tree,geo-point-in-polygon -phase compile -rounds 8 -budget 200ms > experiments/arm64-parity/equality-countdown-bitmask-compile.jsonl 2>&1
python3 experiments/arm64-parity/summarize_paired.py experiments/arm64-parity/equality-countdown-bitmask-compile.jsonl > experiments/arm64-parity/equality-countdown-bitmask-compile-summary.md
