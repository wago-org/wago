#!/bin/sh
set -eu
export WAGO_SHARED_SCALAR=0 WAGO_ARM64_EXPERIMENT_EQUALITY_COUNTDOWN=0
go test -tags wago_codegenstats,wago_regalloccheck,wago_guardpage ./src/core/compiler/backend/railshot/arm64 -run '^TestEqualityCountdown' -v > experiments/arm64-parity/equality-countdown-padding-tests.txt 2>&1
go build -tags wago_guardpage -o /tmp/paired-equality-countdown-padding ./experiments/arm64-parity/paired
for phase in exec compile; do
 /tmp/paired-equality-countdown-padding -option equality-countdown -workloads bio-lcs,document-edit-distance,grid-pathfinding,serialization-protobuf -phase "$phase" -rounds 8 -budget 200ms -code-dir /tmp/equality-countdown-padding-code > "experiments/arm64-parity/equality-countdown-padding-$phase.jsonl" 2>&1
done
python3 experiments/arm64-parity/summarize_paired.py experiments/arm64-parity/equality-countdown-padding-exec.jsonl experiments/arm64-parity/equality-countdown-padding-compile.jsonl > experiments/arm64-parity/equality-countdown-padding-summary.md
