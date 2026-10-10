#!/bin/sh
set -eu
export WAGO_SHARED_SCALAR=0 WAGO_ARM64_EXPERIMENT_LEAF_SCOPED_CONST=0
go build -tags wago_guardpage -o /tmp/paired-leaf-scoped-const ./experiments/arm64-parity/paired
for phase in compile exec; do
 /tmp/paired-leaf-scoped-const -option leaf-scoped-int-const -workloads compiler-register-allocation,vision-components,video-dct,bio-global-alignment,bio-local-alignment,db-btree,vision-distance-transform -phase "$phase" -rounds 8 -budget 200ms > "experiments/arm64-parity/leaf-scoped-const-paired-$phase.jsonl" 2>&1
done
