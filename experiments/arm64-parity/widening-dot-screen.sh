#!/bin/sh
set -eu
export WAGO_SHARED_SCALAR=0
root=/Users/work/Code/Wago/wago/experiments/arm64-parity
cd /tmp/wago-widening-dot-37174169
gofmt -w src/core/compiler/backend/railshot/arm64/widening_dot.go src/core/compiler/backend/railshot/arm64/widening_dot_arm64_test.go
go test -tags wago_codegenstats,wago_regalloccheck,wago_guardpage ./src/core/compiler/backend/railshot/arm64 -run '^TestWideningDot' -count=1 > "$root/widening-dot-strict.txt" 2>&1
go build -tags wago_guardpage -o /tmp/paired-widening-dot ./experiments/arm64-parity/paired
/tmp/paired-widening-dot -core-exec -corpus /Users/work/Code/Web/wasm.fyi/.wasmbench/corpus-cache -option widening-dot-vector -workloads modular-arithmetic -phase exec -rounds 8 -budget 150ms -code-dir /tmp/widening-dot-images > "$root/widening-dot-screen-exec.jsonl" 2>&1
/tmp/paired-widening-dot -core-compile -corpus /Users/work/Code/Web/wasm.fyi/.wasmbench/corpus-cache -option widening-dot-vector -workloads modular-arithmetic -phase compile -rounds 8 -budget 150ms -code-dir /tmp/widening-dot-images > "$root/widening-dot-screen-compile.jsonl" 2>&1
for phase in exec compile; do
 /tmp/paired-widening-dot -option widening-dot-vector -workloads compiler-register-allocation,vision-dilation,graphics-reed-solomon -phase "$phase" -rounds 6 -budget 100ms -code-dir /tmp/widening-dot-images > "$root/widening-dot-control-$phase.jsonl" 2>&1
done
python3 "$root/summarize_paired.py" "$root/widening-dot-screen-exec.jsonl" "$root/widening-dot-screen-compile.jsonl" > "$root/widening-dot-screen-summary.md"
python3 "$root/summarize_paired.py" "$root/widening-dot-control-exec.jsonl" "$root/widening-dot-control-compile.jsonl" > "$root/widening-dot-control-summary.md"
