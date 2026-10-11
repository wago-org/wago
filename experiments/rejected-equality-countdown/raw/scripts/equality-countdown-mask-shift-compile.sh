#!/bin/sh
set -eu
export WAGO_SHARED_SCALAR=0 WAGO_ARM64_EXPERIMENT_EQUALITY_COUNTDOWN=0
unset WAGO_ARM64_EQUALITY_COUNTDOWN_DEAD_ONLY
go test -tags wago_codegenstats,wago_regalloccheck,wago_guardpage ./src/core/compiler/backend/railshot/arm64 -run '^TestEqualityCountdown' -v > experiments/arm64-parity/equality-countdown-mask-shift-tests.txt 2>&1
go build -tags wago_guardpage -o /tmp/paired-equality-countdown-mask-shift ./experiments/arm64-parity/paired
/tmp/paired-equality-countdown-mask-shift -option equality-countdown -workloads compression-rle,bio-lcs,grid-pathfinding,vision-dilation,files-merkle-tree,geo-point-in-polygon -phase compile -rounds 8 -budget 200ms > experiments/arm64-parity/equality-countdown-mask-shift-compile.jsonl 2>&1
python3 experiments/arm64-parity/summarize_paired.py experiments/arm64-parity/equality-countdown-mask-shift-compile.jsonl > experiments/arm64-parity/equality-countdown-mask-shift-compile-summary.md
