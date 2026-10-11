#!/bin/sh
set -eu
export WAGO_SHARED_SCALAR=0 WAGO_ARM64_EXPERIMENT_DOMINATED_INDEXED_KEY=0
cache=/Users/work/Code/Web/wasm.fyi/.wasmbench/corpus-cache
for phase in exec compile; do
 /tmp/paired-dominated-indexed-key -option dominated-indexed-key -workloads compiler-register-allocation,image-median,ml-knn,search-rabin-karp -phase "$phase" -rounds 12 -budget 300ms > "experiments/arm64-parity/dominated-indexed-key-confirm-$phase.jsonl" 2>&1
 /tmp/paired-dominated-indexed-key -core-"$phase" -phase "$phase" -corpus "$cache" -option dominated-indexed-key -workloads modular-arithmetic,complex-roundtrip,compile-match,step,render -rounds 8 -budget 200ms > "experiments/arm64-parity/dominated-indexed-key-confirm-core-$phase.jsonl" 2>&1
done
python3 experiments/arm64-parity/summarize_paired.py experiments/arm64-parity/dominated-indexed-key-confirm-*.jsonl > experiments/arm64-parity/dominated-indexed-key-confirm-summary.md
