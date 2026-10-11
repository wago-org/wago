#!/bin/sh
set -eu
export WAGO_SHARED_SCALAR=0 WAGO_ARM64_EXPERIMENT_POSITIVE_DIVISOR_TRAPS=0
export WAGO_PARITY_CACHE=/Users/work/Code/Web/wasm.fyi/.wasmbench/corpus-cache
[ -s experiments/arm64-parity/positive-divisor-native-changes.txt ]
go build -tags wago_guardpage -o /tmp/paired-positive-divisor-summary ./experiments/arm64-parity/paired
for phase in exec compile; do
 /tmp/paired-positive-divisor-summary -option positive-divisor-traps -workloads audio-adpcm,numeric-euclidean-gcd,compiler-register-allocation -phase "$phase" -rounds 8 -budget 300ms > "experiments/arm64-parity/positive-divisor-summary-paired-$phase.jsonl" 2>&1
 /tmp/paired-positive-divisor-summary -core-"$phase" -phase "$phase" -corpus "$WAGO_PARITY_CACHE" -option positive-divisor-traps -workloads decimal-parse,modular-arithmetic -rounds 8 -budget 200ms > "experiments/arm64-parity/positive-divisor-summary-core-paired-$phase.jsonl" 2>&1
done
python3 experiments/arm64-parity/summarize_paired.py experiments/arm64-parity/positive-divisor-summary-paired-exec.jsonl experiments/arm64-parity/positive-divisor-summary-paired-compile.jsonl experiments/arm64-parity/positive-divisor-summary-core-paired-exec.jsonl experiments/arm64-parity/positive-divisor-summary-core-paired-compile.jsonl > experiments/arm64-parity/positive-divisor-summary-paired-summary.md
