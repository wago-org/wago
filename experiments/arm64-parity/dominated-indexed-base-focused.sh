#!/bin/sh
set -eu
export WAGO_SHARED_SCALAR=0 WAGO_ARM64_EXPERIMENT_DOMINATED_INDEXED_BASE=0
export WAGO_PARITY_CACHE=/Users/work/Code/Web/wasm.fyi/.wasmbench/corpus-cache WAGO_PARITY_CORPUS=/Users/work/Code/Web/wasm.fyi/corpora/applications
go test -tags wago_codegenstats,wago_regalloccheck,wago_guardpage ./src/core/compiler/backend/railshot/arm64 > experiments/arm64-parity/dominated-indexed-base-loop-tests.txt 2>&1
go build -tags wago_guardpage -o /tmp/paired-dominated-indexed-base ./experiments/arm64-parity/paired
apps=compiler-register-allocation,image-median,ml-kmeans,ml-knn,search-horspool,search-kmp,search-rabin-karp,stats-gini
for phase in compile exec;do
 /tmp/paired-dominated-indexed-base -option dominated-indexed-base -workloads "$apps" -phase "$phase" -rounds 8 -budget 200ms > "experiments/arm64-parity/dominated-indexed-base-paired-$phase.jsonl" 2>&1
done
/tmp/paired-dominated-indexed-base -core-compile -corpus "$WAGO_PARITY_CACHE" -option dominated-indexed-base -workloads decimal-parse,complex-roundtrip,compile-match -phase compile -rounds 8 -budget 200ms > experiments/arm64-parity/dominated-indexed-base-core-paired-compile.jsonl 2>&1
/tmp/paired-dominated-indexed-base -core-exec -corpus "$WAGO_PARITY_CACHE" -option dominated-indexed-base -workloads complex-roundtrip -phase exec -rounds 8 -budget 200ms > experiments/arm64-parity/dominated-indexed-base-core-paired-exec.jsonl 2>&1
