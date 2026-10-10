#!/bin/sh
set -eu
export WAGO_SHARED_SCALAR=0
export WAGO_PARITY_CORPUS=/Users/work/Code/Web/wasm.fyi/corpora/applications
go test -overlay /tmp/code-buffer-adopt-overlay.json -tags wago_codegenstats,wago_regalloccheck,wago_guardpage ./src/core/compiler/backend/railshot ./src/core/compiler/backend/railshot/arm64 ./src/core/runtime ./src/core/encoder/arm64 ./src/core/compiler/optimization > experiments/arm64-parity/code-buffer-adopt-tests.txt 2>&1
go test -overlay /tmp/code-buffer-adopt-overlay.json -tags wago_guardpage -c -o /tmp/parity-code-buffer-adopt.test ./experiments/arm64-parity
for variant in before after; do
 binary=/tmp/parity-store-map-pointers.test
 if [ "$variant" = after ]; then binary=/tmp/parity-code-buffer-adopt.test; fi
 WAGO_ARM64_EXPERIMENT_STORE_MAP=1 WAGO_PARITY_CODE_DIR=/tmp/code-buffer-adopt-$variant "$binary" -test.run=^$ -test.bench='BenchmarkParity/(ml-inference|vision-components|image-resize|db-sort-merge-join|video-dct|compiler-register-allocation|search-aho-corasick)/Exec$' -test.benchtime=1x > "experiments/arm64-parity/code-buffer-adopt-$variant-oracles.txt" 2>&1
done
for image in /tmp/code-buffer-adopt-before/*.bin; do
 cmp "$image" "/tmp/code-buffer-adopt-after/${image##*/}"
done
index=0
for variant in off-before off-after on-before on-after on-after on-before off-after off-before; do
 binary=/tmp/parity-store-map-pointers.test
 case "$variant" in *-after) binary=/tmp/parity-code-buffer-adopt.test;; esac
 flag=0
 case "$variant" in on-*) flag=1;; esac
 WAGO_ARM64_EXPERIMENT_STORE_MAP=$flag "$binary" -test.run=^$ -test.bench='BenchmarkParity/(ml-inference|vision-components|image-resize|db-sort-merge-join|video-dct|compiler-register-allocation|search-aho-corasick)/Compile$' -test.benchtime=200ms -test.count=2 > "experiments/arm64-parity/code-buffer-adopt-$index-$variant.txt" 2>&1
 index=$((index+1))
done
