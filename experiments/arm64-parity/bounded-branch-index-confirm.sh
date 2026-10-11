#!/bin/sh
set -eu
export WAGO_SHARED_SCALAR=0
root=/Users/work/Code/Wago/wago/experiments/arm64-parity
/tmp/paired-bounded-branch-index -option bounded-branch-index -workloads language-register-vm -phase exec -rounds 10 -budget 300ms > "$root/bounded-branch-index-confirm-exec.jsonl" 2>&1
python3 "$root/summarize_paired.py" "$root/bounded-branch-index-confirm-exec.jsonl" > "$root/bounded-branch-index-confirm-summary.md"
python3 "$root/bounded-branch-index-cross.py"
