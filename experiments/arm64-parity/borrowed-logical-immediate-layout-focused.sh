#!/bin/sh
set -eu
export WAGO_ARM64_BORROWED_LOGICAL_PRESERVE_LAYOUT=1 WAGO_SHARED_SCALAR=0 WAGO_ARM64_EXPERIMENT_BORROWED_LOGICAL_IMMEDIATE=0
export WAGO_PARITY_CACHE=/Users/work/Code/Web/wasm.fyi/.wasmbench/corpus-cache WAGO_PARITY_CORPUS=/Users/work/Code/Web/wasm.fyi/corpora/applications
go test -tags wago_codegenstats,wago_regalloccheck,wago_guardpage ./src/core/compiler/backend/railshot/arm64 > experiments/arm64-parity/borrowed-logical-immediate-layout-tests.txt 2>&1
go build -tags wago_guardpage -o /tmp/paired-borrowed-logical-layout ./experiments/arm64-parity/paired
apps=audio-adpcm,serialization-protobuf,language-register-vm,compiler-register-allocation,ml-knn
for phase in compile exec;do
 /tmp/paired-borrowed-logical-layout -option borrowed-logical-immediate -workloads "$apps" -phase "$phase" -rounds 8 -budget 200ms > "experiments/arm64-parity/borrowed-logical-immediate-layout-paired-$phase.jsonl" 2>&1
done
