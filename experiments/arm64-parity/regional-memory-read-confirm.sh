#!/bin/sh
set -eu
export WAGO_SHARED_SCALAR=0 WAGO_ARM64_EXPERIMENT_REGIONAL_MEMORY_READ=0
for phase in exec compile; do
 /tmp/paired-regional-memory-read -option regional-memory-read -workloads graphics-reed-solomon,compiler-register-allocation -phase "$phase" -rounds 12 -budget 300ms > "experiments/arm64-parity/regional-memory-read-confirm-$phase.jsonl" 2>&1
done
python3 experiments/arm64-parity/summarize_paired.py experiments/arm64-parity/regional-memory-read-confirm-exec.jsonl experiments/arm64-parity/regional-memory-read-confirm-compile.jsonl > experiments/arm64-parity/regional-memory-read-confirm-summary.md
