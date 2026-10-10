#!/bin/sh
set -eu
export WAGO_SHARED_SCALAR=0 WAGO_ARM64_EXPERIMENT_DOMINATED_INDEXED_BASE=0
/tmp/paired-dominated-indexed-base-indexed -core-exec -corpus /Users/work/Code/Web/wasm.fyi/.wasmbench/corpus-cache -option dominated-indexed-base -workloads modular-arithmetic,aead-blake2b -phase exec -rounds 12 -budget 300ms > experiments/arm64-parity/dominated-indexed-base-upstream-repeat.jsonl 2>&1
