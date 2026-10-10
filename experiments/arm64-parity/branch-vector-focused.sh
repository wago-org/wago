#!/bin/sh
set -eu
export WAGO_SHARED_SCALAR=0 WAGO_ARM64_EXPERIMENT_BR_TABLE_BRANCH_VECTOR=0
export WAGO_PARITY_CACHE=/Users/work/Code/Web/wasm.fyi/.wasmbench/corpus-cache
go build -tags wago_guardpage -o /tmp/paired-branch-vector ./experiments/arm64-parity/paired
for phase in compile exec; do
 /tmp/paired-branch-vector -option br-table-branch-vector -workloads language-register-vm -phase "$phase" -rounds 12 -budget 300ms > "experiments/arm64-parity/branch-vector-paired-$phase.jsonl" 2>&1
 /tmp/paired-branch-vector -core-"$phase" -corpus "$WAGO_PARITY_CACHE" -option br-table-branch-vector -workloads compile-match -phase "$phase" -rounds 8 -budget 200ms > "experiments/arm64-parity/branch-vector-core-paired-$phase.jsonl" 2>&1
done
