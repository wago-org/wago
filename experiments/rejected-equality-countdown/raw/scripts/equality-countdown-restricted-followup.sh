#!/bin/sh
set -eu
export WAGO_SHARED_SCALAR=0 WAGO_ARM64_EXPERIMENT_EQUALITY_COUNTDOWN=0
for phase in exec compile; do
 /tmp/paired-equality-countdown-restricted -option equality-countdown -workloads serialization-protobuf,document-edit-distance,db-btree,document-page-packing -phase "$phase" -rounds 8 -budget 200ms > "experiments/arm64-parity/equality-countdown-restricted-followup-$phase.jsonl" 2>&1
done
/tmp/paired-equality-countdown-restricted -corpus /Users/work/Code/Web/wasm.fyi/.wasmbench/corpus-cache -core-exec -option equality-countdown -workloads decimal-parse,parse-edit-write -phase exec -rounds 8 -budget 150ms > experiments/arm64-parity/equality-countdown-restricted-followup-core-exec.jsonl 2>&1
/tmp/paired-equality-countdown-restricted -corpus /Users/work/Code/Web/wasm.fyi/.wasmbench/corpus-cache -core-compile -option equality-countdown -workloads decimal-parse,parse-edit-write,inflate -phase compile -rounds 4 -budget 100ms > experiments/arm64-parity/equality-countdown-restricted-followup-core-compile.jsonl 2>&1
python3 experiments/arm64-parity/summarize_paired.py experiments/arm64-parity/equality-countdown-restricted-followup-exec.jsonl experiments/arm64-parity/equality-countdown-restricted-followup-compile.jsonl experiments/arm64-parity/equality-countdown-restricted-followup-core-exec.jsonl experiments/arm64-parity/equality-countdown-restricted-followup-core-compile.jsonl > experiments/arm64-parity/equality-countdown-restricted-followup-summary.md
