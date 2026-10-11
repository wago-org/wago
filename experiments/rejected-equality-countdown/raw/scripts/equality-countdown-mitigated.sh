#!/bin/sh
set -eu
export WAGO_SHARED_SCALAR=0 WAGO_ARM64_EQUALITY_COUNTDOWN_CBNZ=1
export WAGO_ARM64_EXPERIMENT_EQUALITY_COUNTDOWN=0
# Native matrix exercises the new branch form independently.
go test -tags wago_codegenstats,wago_regalloccheck,wago_guardpage ./src/core/compiler/backend/railshot/arm64 -run '^TestEqualityCountdown' -v > experiments/arm64-parity/equality-countdown-mitigated-tests.txt 2>&1
go build -tags wago_guardpage -o /tmp/paired-equality-countdown-mitigated ./experiments/arm64-parity/paired
for phase in exec compile; do
 /tmp/paired-equality-countdown-mitigated -option equality-countdown -workloads audio-adpcm,compiler-register-allocation,bio-global-alignment,audio-autocorrelation,bio-kmer-count,geo-point-in-polygon -phase "$phase" -rounds 8 -budget 200ms > "experiments/arm64-parity/equality-countdown-mitigated-$phase.jsonl" 2>&1
done
python3 experiments/arm64-parity/summarize_paired.py experiments/arm64-parity/equality-countdown-mitigated-exec.jsonl experiments/arm64-parity/equality-countdown-mitigated-compile.jsonl > experiments/arm64-parity/equality-countdown-mitigated-summary.md
