#!/bin/sh
set -eu
export WAGO_SHARED_SCALAR=0 WAGO_ARM64_EXPERIMENT_EQUALITY_COUNTDOWN=0
cache=/Users/work/Code/Web/wasm.fyi/.wasmbench/corpus-cache
/tmp/paired-equality-countdown-final -core-exec -phase exec -corpus "$cache" -option equality-countdown -workloads parse-edit-write,decimal-parse,compile-match -rounds 8 -budget 100ms > experiments/arm64-parity/equality-countdown-core-exec.jsonl 2>&1
python3 experiments/arm64-parity/summarize_paired.py experiments/arm64-parity/equality-countdown-core-exec.jsonl experiments/arm64-parity/equality-countdown-core-compile.jsonl > experiments/arm64-parity/equality-countdown-core-summary.md
