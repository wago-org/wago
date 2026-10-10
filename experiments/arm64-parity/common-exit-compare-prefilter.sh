#!/bin/sh
set -eu
export WAGO_SHARED_SCALAR=0 WAGO_ARM64_EXPERIMENT_COMMON_EXIT_COMPARE=0
export WAGO_PARITY_CACHE=/Users/work/Code/Web/wasm.fyi/.wasmbench/corpus-cache
go test -tags wago_codegenstats,wago_regalloccheck,wago_guardpage ./src/core/compiler/backend/railshot/arm64 -run CommonExit -count=1 > experiments/arm64-parity/common-exit-compare-prefilter-tests.txt 2>&1
go build -tags wago_guardpage -o /tmp/paired-common-exit-compare-prefilter ./experiments/arm64-parity/paired
for phase in compile exec; do
 /tmp/paired-common-exit-compare-prefilter -option common-exit-compare -workloads vision-dilation,graphics-bresenham,search-rabin-karp -phase "$phase" -rounds 12 -budget 300ms > "experiments/arm64-parity/common-exit-compare-prefilter-paired-$phase.jsonl" 2>&1
done
/tmp/paired-common-exit-compare-prefilter -core-compile -corpus "$WAGO_PARITY_CACHE" -option common-exit-compare -workloads modular-arithmetic,complex-roundtrip -phase compile -rounds 8 -budget 200ms > experiments/arm64-parity/common-exit-compare-prefilter-core-paired-compile.jsonl 2>&1
