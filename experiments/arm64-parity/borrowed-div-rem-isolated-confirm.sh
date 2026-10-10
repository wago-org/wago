#!/bin/sh
set -eu
cd /tmp/wago-borrowed-div-rem-isolated-20261010
export GOWORK=off WAGO_SHARED_SCALAR=0 WAGO_ARM64_EXPERIMENT_LEAF_SCOPED_CONST=0 WAGO_ARM64_EXPERIMENT_BORROWED_DIV_REM=0
RESULTS=/Users/work/Code/Wago/wago/experiments/arm64-parity
/tmp/paired-borrowed-div-rem-isolated -option borrowed-div-rem -workloads numeric-euclidean-gcd,db-btree,game-collision-impulse -phase exec -rounds 12 -budget 300ms > "$RESULTS/borrowed-div-rem-isolated-confirm-paired-exec.jsonl" 2>&1
for phase in compile exec; do
 /tmp/paired-borrowed-div-rem-isolated -option borrowed-div-rem -workloads ml-kmeans,language-register-vm,video-optical-flow,stencil-wave-equation,ray-box,stats-bootstrap -phase "$phase" -rounds 8 -budget 200ms > "$RESULTS/borrowed-div-rem-isolated-expanded-paired-$phase.jsonl" 2>&1
done
