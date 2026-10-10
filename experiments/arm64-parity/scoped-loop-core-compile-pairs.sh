#!/bin/sh
set -eu
export WAGO_SHARED_SCALAR=0 WAGO_ARM64_EXPERIMENT_SCOPED_LOOP_CONST=0 WAGO_ARM64_EXPERIMENT_SMALL_MUL_SHIFT=0
go build -tags wago_guardpage -o /tmp/paired-scoped-loop-core ./experiments/arm64-parity/paired
/tmp/paired-scoped-loop-core -core-compile -corpus /Users/work/Code/Web/wasm.fyi/.wasmbench/corpus-cache -option scoped-loop-int-const -workloads hash,decimal-parse,compile-match,2k-performance,serializeN -phase compile -rounds 10 -budget 250ms > experiments/arm64-parity/scoped-loop-core-compile-pairs.jsonl 2>&1
