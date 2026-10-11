#!/bin/sh
set -eu
export WAGO_SHARED_SCALAR=0 WAGO_ARM64_EXPERIMENT_POSITIVE_DIVISOR_TRAPS=0 WAGO_ARM64_POSITIVE_DIVISOR_SHRINK=1
export WAGO_PARITY_CACHE=/Users/work/Code/Web/wasm.fyi/.wasmbench/corpus-cache
[ -s experiments/arm64-parity/positive-divisor-shrink-native-changes.txt ]
go build -tags wago_guardpage -o /tmp/paired-positive-divisor-shrink ./experiments/arm64-parity/paired
for phase in exec compile; do
 /tmp/paired-positive-divisor-shrink -option positive-divisor-traps -workloads audio-adpcm,numeric-euclidean-gcd,compiler-register-allocation -phase "$phase" -rounds 12 -budget 400ms > "experiments/arm64-parity/positive-divisor-shrink-paired-$phase.jsonl" 2>&1
 /tmp/paired-positive-divisor-shrink -core-"$phase" -phase "$phase" -corpus "$WAGO_PARITY_CACHE" -option positive-divisor-traps -workloads decimal-parse,modular-arithmetic -rounds 6 -budget 200ms > "experiments/arm64-parity/positive-divisor-shrink-core-paired-$phase.jsonl" 2>&1
done
python3 experiments/arm64-parity/summarize_paired.py experiments/arm64-parity/positive-divisor-shrink-paired-exec.jsonl experiments/arm64-parity/positive-divisor-shrink-paired-compile.jsonl experiments/arm64-parity/positive-divisor-shrink-core-paired-exec.jsonl experiments/arm64-parity/positive-divisor-shrink-core-paired-compile.jsonl > experiments/arm64-parity/positive-divisor-shrink-paired-summary.md
