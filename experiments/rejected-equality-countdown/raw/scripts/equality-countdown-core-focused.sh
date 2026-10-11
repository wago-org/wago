#!/bin/sh
set -eu
export WAGO_SHARED_SCALAR=0 WAGO_ARM64_EXPERIMENT_EQUALITY_COUNTDOWN=0
cache=/Users/work/Code/Web/wasm.fyi/.wasmbench/corpus-cache
/tmp/paired-equality-countdown-final -core-compile -phase compile -corpus "$cache" -option equality-countdown -workloads hash,encode,inflate,decompress,parse-edit-write,decimal-parse,compile-match -rounds 4 -budget 100ms > experiments/arm64-parity/equality-countdown-core-compile.jsonl 2>&1
/tmp/paired-equality-countdown-final -core-exec -phase exec -corpus "$cache" -option equality-countdown -workloads hash,encode,parse-edit-write,decimal-parse,compile-match -rounds 4 -budget 100ms > experiments/arm64-parity/equality-countdown-core-exec.jsonl 2>&1
python3 experiments/arm64-parity/summarize_paired.py experiments/arm64-parity/equality-countdown-core-exec.jsonl experiments/arm64-parity/equality-countdown-core-compile.jsonl > experiments/arm64-parity/equality-countdown-core-summary.md
