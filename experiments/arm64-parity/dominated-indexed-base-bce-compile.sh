#!/bin/sh
set -eu
export WAGO_SHARED_SCALAR=0 WAGO_ARM64_EXPERIMENT_DOMINATED_INDEXED_BASE=0
export WAGO_PARITY_CACHE=/Users/work/Code/Web/wasm.fyi/.wasmbench/corpus-cache
go build -tags wago_guardpage -o /tmp/paired-dominated-indexed-base-bce ./experiments/arm64-parity/paired
for phase in compile;do
 /tmp/paired-dominated-indexed-base-bce -option dominated-indexed-base -workloads image-median,ml-knn,search-horspool,search-rabin-karp -phase "$phase" -rounds 12 -budget 200ms > "experiments/arm64-parity/dominated-indexed-base-bce-confirm-$phase.jsonl" 2>&1
 if [ "$phase" = compile ];then mode=-core-compile;else mode=-core-exec;fi
 /tmp/paired-dominated-indexed-base-bce "$mode" -corpus "$WAGO_PARITY_CACHE" -option dominated-indexed-base -workloads modular-arithmetic,aead-blake2b,compile-match,parse-edit-write -phase "$phase" -rounds 8 -budget 200ms > "experiments/arm64-parity/dominated-indexed-base-bce-upstream-$phase.jsonl" 2>&1
done
