#!/bin/sh
set -eu
export WAGO_SHARED_SCALAR=0 WAGO_ARM64_EXPERIMENT_LEAF_SCOPED_CONST=0
go build -tags wago_guardpage -o /tmp/paired-leaf-scoped-const-filtered ./experiments/arm64-parity/paired
for phase in compile exec; do
 /tmp/paired-leaf-scoped-const-filtered -option leaf-scoped-int-const -workloads compiler-register-allocation,vision-components,bio-global-alignment,bio-local-alignment,vision-distance-transform -phase "$phase" -rounds 8 -budget 200ms > "experiments/arm64-parity/leaf-scoped-const-filtered-paired-$phase.jsonl" 2>&1
done
