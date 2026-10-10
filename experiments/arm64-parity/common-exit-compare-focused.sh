#!/bin/sh
set -eu
export WAGO_SHARED_SCALAR=0 WAGO_ARM64_EXPERIMENT_COMMON_EXIT_COMPARE=0
export WAGO_PARITY_CACHE=/Users/work/Code/Web/wasm.fyi/.wasmbench/corpus-cache
go build -tags wago_guardpage -o /tmp/paired-common-exit-compare ./experiments/arm64-parity/paired
for phase in compile exec;do
 /tmp/paired-common-exit-compare -option common-exit-compare -workloads vision-dilation,graphics-bresenham,search-rabin-karp -phase "$phase" -rounds 12 -budget 300ms > "experiments/arm64-parity/common-exit-compare-paired-$phase.jsonl" 2>&1
done
/tmp/paired-common-exit-compare -core-compile -corpus "$WAGO_PARITY_CACHE" -option common-exit-compare -workloads modular-arithmetic,compile-match,parse-structure,complex-roundtrip -phase compile -rounds 8 -budget 200ms > experiments/arm64-parity/common-exit-compare-core-paired-compile.jsonl 2>&1
/tmp/paired-common-exit-compare -core-exec -corpus "$WAGO_PARITY_CACHE" -option common-exit-compare -workloads modular-arithmetic,compile-match,normalize-casefold -phase exec -rounds 8 -budget 200ms > experiments/arm64-parity/common-exit-compare-core-paired-exec.jsonl 2>&1
