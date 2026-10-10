#!/bin/sh
set -eu
export WAGO_SHARED_SCALAR=0
export WAGO_PARITY_CORPUS=/Users/work/Code/Web/wasm.fyi/corpora/applications
index=0
for mode in off on on off; do
 flag=0
 if [ "$mode" = on ]; then flag=1; fi
 WAGO_ARM64_EXPERIMENT_STORE_MAP=$flag /tmp/parity-store-map-constant-alias.test -test.run=^$ -test.bench='BenchmarkParity/(image-composite|language-register-vm|ml-inference|image-resize|video-temporal-denoise|files-path-trie|stats-bootstrap|audio-autocorrelation|ml-max-pooling|image-error-diffusion|stats-quickselect|document-bidi-reorder|ml-convolution|db-sort-merge-join|vision-components|video-deblocking|vision-dilation|compiler-register-allocation|video-dct)/(Compile|Exec)$' -test.benchtime=200ms -test.count=2 > "experiments/arm64-parity/store-map-current-$index-$mode.txt" 2>&1
 index=$((index+1))
done
