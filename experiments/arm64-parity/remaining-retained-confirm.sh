#!/bin/sh
set -eu
export WAGO_SHARED_SCALAR=0 WAGO_ARM64_EXPERIMENT_BR_TABLE_BRANCH_VECTOR=0
/tmp/paired-branch-vector -option br-table-branch-vector -workloads ml-knn,language-register-vm,audio-adpcm -phase exec -rounds 8 -budget 300ms > experiments/arm64-parity/remaining-retained-confirm.jsonl 2>&1
python3 experiments/arm64-parity/summarize_paired.py experiments/arm64-parity/remaining-retained-confirm.jsonl > experiments/arm64-parity/remaining-retained-confirm-summary.md
