#!/bin/sh
set -eu
export WAGO_SHARED_SCALAR=0 WAGO_ARM64_EXPERIMENT_REGIONAL_MEMORY_READ=0
cache=/Users/work/Code/Web/wasm.fyi/.wasmbench/corpus-cache
go build -tags wago_guardpage -o /tmp/paired-regional-memory-vector ./experiments/arm64-parity/paired
for length in 1024 16384 102400; do
 /tmp/paired-regional-memory-vector -core-exec -phase exec -corpus "$cache" -option regional-memory-read -workloads hash -vector-length "$length" -rounds 12 -budget 300ms > "experiments/arm64-parity/regional-memory-vector-confirm-$length.jsonl" 2>&1
 python3 experiments/arm64-parity/summarize_paired.py "experiments/arm64-parity/regional-memory-vector-confirm-$length.jsonl" > "experiments/arm64-parity/regional-memory-vector-confirm-$length-summary.md"
done
