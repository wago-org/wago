#!/bin/sh
set -eu
export WAGO_SHARED_SCALAR=0
root=/Users/work/Code/Wago/wago/experiments/arm64-parity
cd /tmp/wago-taken-branch-37174169
go build -tags wago_guardpage -o /tmp/paired-taken-zero-branch ./experiments/arm64-parity/paired
for phase in exec compile; do
 /tmp/paired-taken-zero-branch -option taken-zero-branch -workloads hardware-boolean-anf -phase "$phase" -rounds 8 -budget 200ms -code-dir /tmp/taken-zero-branch-screen-images > "$root/taken-zero-branch-screen-$phase.jsonl" 2>&1
 /tmp/paired-taken-zero-branch -option taken-zero-branch -workloads compiler-register-allocation,files-glob-match,audio-adpcm -phase "$phase" -rounds 4 -budget 100ms > "$root/taken-zero-branch-controls-$phase.jsonl" 2>&1
done
python3 "$root/summarize_paired.py" "$root/taken-zero-branch-screen-exec.jsonl" "$root/taken-zero-branch-screen-compile.jsonl" "$root/taken-zero-branch-controls-exec.jsonl" "$root/taken-zero-branch-controls-compile.jsonl" > "$root/taken-zero-branch-screen-summary.md"
