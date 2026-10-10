#!/bin/sh
set -eu
export WAGO_SHARED_SCALAR=0
go test -tags wago_codegenstats,wago_regalloccheck,wago_guardpage ./src/core/compiler/backend/railshot ./src/core/compiler/backend/railshot/arm64 ./src/core/runtime ./src/core/encoder/arm64 ./src/core/compiler/optimization > experiments/arm64-parity/store-map-policy-tests.txt 2>&1
go build -tags wago_guardpage -o /tmp/store-map-paired ./experiments/arm64-parity/paired
/tmp/store-map-paired -option store-map-vector -workloads ml-inference,vision-components,image-resize,db-sort-merge-join,stats-bootstrap,ml-max-pooling,files-path-trie,audio-autocorrelation -rounds 6 -budget 100ms -code-dir /tmp/store-map-policy-paired > experiments/arm64-parity/store-map-policy-paired.jsonl
