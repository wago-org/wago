#!/bin/sh
set -eu
export WAGO_SHARED_SCALAR=0 WAGO_ARM64_EXPERIMENT_LEAF_SCOPED_CONST=0 WAGO_ARM64_EXPERIMENT_BORROWED_DIV_REM=0
RESULTS=/Users/work/Code/Wago/wago/experiments/arm64-parity
for spec in 'concrete 1' 'owned-result 1' 'owned-result 2' 'concrete 2'; do
 set -- $spec
 /tmp/paired-borrowed-div-rem-$1 -option borrowed-div-rem -workloads numeric-euclidean-gcd,language-register-vm -phase exec -rounds 12 -budget 300ms > "$RESULTS/borrowed-div-rem-threeway-$1-$2.jsonl" 2>&1
done
