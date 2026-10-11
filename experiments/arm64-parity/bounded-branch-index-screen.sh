#!/bin/sh
set -eu
export WAGO_SHARED_SCALAR=0
root=/Users/work/Code/Wago/wago/experiments/arm64-parity
cd /tmp/wago-bounded-dispatch-37174169
gofmt -w src/core/compiler/backend/railshot/arm64/bounded_branch_index.go src/core/compiler/backend/railshot/arm64/bounded_branch_index_arm64_test.go src/core/compiler/backend/railshot/arm64/knobs.go src/core/compiler/backend/railshot/arm64/control.go src/core/compiler/backend/railshot/arm64/br_table_branch_vector.go src/core/compiler/optimization/catalog.go
go test -tags wago_codegenstats,wago_regalloccheck,wago_guardpage ./src/core/compiler/backend/railshot/arm64 -run '^TestBoundedBranchIndexNative$' -count=1 > "$root/bounded-branch-index-strict.txt" 2>&1
go build -tags wago_guardpage -o /tmp/paired-bounded-branch-index ./experiments/arm64-parity/paired
for phase in exec compile; do
 /tmp/paired-bounded-branch-index -option bounded-branch-index -workloads language-register-vm,compiler-register-allocation,hardware-boolean-anf,files-glob-match -phase "$phase" -rounds 8 -budget 150ms -code-dir /tmp/bounded-branch-index-images > "$root/bounded-branch-index-screen-$phase.jsonl" 2>&1
done
python3 "$root/summarize_paired.py" "$root/bounded-branch-index-screen-exec.jsonl" "$root/bounded-branch-index-screen-compile.jsonl" > "$root/bounded-branch-index-screen-summary.md"
