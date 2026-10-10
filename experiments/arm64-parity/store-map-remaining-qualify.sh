#!/bin/sh
set -eu
export WAGO_SHARED_SCALAR=0 WAGO_PARITY_CORPUS=/Users/work/Code/Web/wasm.fyi/corpora/applications
index=0
for mode in off on on off; do
 flag=0
 if [ "$mode" = on ]; then flag=1; fi
 WAGO_ARM64_EXPERIMENT_STORE_MAP=$flag /tmp/parity-store-map-affine.test -test.run=^$ -test.bench='BenchmarkParity/(language-register-vm|video-temporal-denoise|files-path-trie|stats-bootstrap|audio-autocorrelation|ml-max-pooling|image-error-diffusion|document-bidi-reorder|db-sort-merge-join|video-deblocking|ml-inference|stats-quickselect)/(Compile|Exec)$' -test.benchtime=100ms -test.count=2 > "experiments/arm64-parity/store-map-remaining-$index-$mode.txt" 2>&1
 index=$((index+1))
done
