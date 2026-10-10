#!/bin/sh
set -eu
export WAGO_SHARED_SCALAR=0 WAGO_ARM64_EXPERIMENT_BORROWED_LOGICAL_IMMEDIATE=0
export WAGO_PARITY_CACHE=/Users/work/Code/Web/wasm.fyi/.wasmbench/corpus-cache WAGO_PARITY_CORPUS=/Users/work/Code/Web/wasm.fyi/corpora/applications
go build -tags wago_guardpage -o /tmp/paired-borrowed-logical-immediate ./experiments/arm64-parity/paired
apps=$(cat experiments/arm64-parity/borrowed-logical-immediate-selected-apps.txt)
for phase in compile exec;do
 /tmp/paired-borrowed-logical-immediate -option borrowed-logical-immediate -workloads "$apps" -phase "$phase" -rounds 8 -budget 200ms > "experiments/arm64-parity/borrowed-logical-immediate-paired-$phase.jsonl" 2>&1
done
/tmp/paired-borrowed-logical-immediate -core-compile -corpus "$WAGO_PARITY_CACHE" -option borrowed-logical-immediate -workloads decimal-parse,complex-roundtrip -phase compile -rounds 8 -budget 200ms > experiments/arm64-parity/borrowed-logical-immediate-core-paired-compile.jsonl 2>&1
/tmp/paired-borrowed-logical-immediate -core-exec -corpus "$WAGO_PARITY_CACHE" -option borrowed-logical-immediate -workloads decimal-parse,complex-roundtrip -phase exec -rounds 8 -budget 200ms > experiments/arm64-parity/borrowed-logical-immediate-core-paired-exec.jsonl 2>&1
