#!/bin/sh
set -eu
export WAGO_SHARED_SCALAR=0
go build -tags wago_guardpage -o /tmp/store-map-paired-phases ./experiments/arm64-parity/paired
/tmp/store-map-paired-phases -option store-map-vector -phase compile -workloads ml-inference,vision-components,image-resize,db-sort-merge-join,stats-bootstrap,ml-max-pooling,files-path-trie,audio-autocorrelation,compiler-register-allocation -rounds 6 -budget 100ms > experiments/arm64-parity/store-map-policy-paired-compile.jsonl
