#!/bin/sh
set -eu
export WAGO_SHARED_SCALAR=0 WAGO_ARM64_EXPERIMENT_DOMINATED_INDEXED_KEY=0
cache=/Users/work/Code/Web/wasm.fyi/.wasmbench/corpus-cache
test -f experiments/arm64-parity/dominated-indexed-key-proof.txt
for phase in exec compile; do
 /tmp/paired-dominated-indexed-key -option dominated-indexed-key -workloads compiler-register-allocation,image-median,ml-knn,ml-kmeans,search-horspool,search-rabin-karp,stats-gini -phase "$phase" -rounds 8 -budget 200ms > "experiments/arm64-parity/dominated-indexed-key-paired-$phase.jsonl" 2>&1
 /tmp/paired-dominated-indexed-key -core-"$phase" -phase "$phase" -corpus "$cache" -option dominated-indexed-key -workloads modular-arithmetic,complex-roundtrip -rounds 8 -budget 200ms > "experiments/arm64-parity/dominated-indexed-key-core-paired-$phase.jsonl" 2>&1
done
/tmp/paired-dominated-indexed-key -core-compile -phase compile -corpus "$cache" -option dominated-indexed-key -workloads compile-match,2k-performance -rounds 8 -budget 200ms > experiments/arm64-parity/dominated-indexed-key-larger-core-paired-compile.jsonl 2>&1
python3 experiments/arm64-parity/summarize_paired.py experiments/arm64-parity/dominated-indexed-key-*paired-*.jsonl > experiments/arm64-parity/dominated-indexed-key-paired-summary.md
