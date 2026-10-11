#!/bin/sh
set -eu
export WAGO_SHARED_SCALAR=0 WAGO_ARM64_EXPERIMENT_BOUNDED_SIGNED_QUOTIENT=0
[ -s experiments/arm64-parity/bounded-quotient-native-changes.txt ]
go build -tags wago_guardpage -o /tmp/paired-bounded-quotient ./experiments/arm64-parity/paired
for phase in exec compile; do
 /tmp/paired-bounded-quotient -option bounded-signed-quotient -workloads audio-adpcm,numeric-euclidean-gcd,compiler-register-allocation -phase "$phase" -rounds 12 -budget 400ms > "experiments/arm64-parity/bounded-quotient-paired-$phase.jsonl" 2>&1
done
python3 experiments/arm64-parity/summarize_paired.py experiments/arm64-parity/bounded-quotient-paired-exec.jsonl experiments/arm64-parity/bounded-quotient-paired-compile.jsonl > experiments/arm64-parity/bounded-quotient-paired-summary.md
