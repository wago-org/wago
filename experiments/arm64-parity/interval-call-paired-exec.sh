#!/bin/sh
set -eu
export WAGO_SHARED_SCALAR=0 WAGO_ARM64_EXPERIMENT_INTERVAL_CALL_REGIONS=0
go build -tags wago_guardpage -o /tmp/paired-interval-call-exec ./experiments/arm64-parity/paired
/tmp/paired-interval-call-exec -core-exec -corpus /Users/work/Code/Web/wasm.fyi/.wasmbench/corpus-cache -option interval-call-regions -workloads decimal-parse,complex-roundtrip -phase exec -rounds 12 -budget 350ms -code-dir /tmp/interval-call-paired-exec > experiments/arm64-parity/interval-call-paired-exec.jsonl 2>&1
