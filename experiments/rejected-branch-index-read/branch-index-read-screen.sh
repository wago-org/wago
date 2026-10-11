#!/bin/sh
set -eu
export WAGO_SHARED_SCALAR=0
unset WAGO_ARM64_NO_BOUNDED_BRANCH_INDEX
root=/Users/work/Code/Wago/wago/experiments/arm64-parity
cd /tmp/wago-branch-index-read-37174169
gofmt -w src/core/compiler/backend/railshot/arm64/branch_index_read.go src/core/compiler/backend/railshot/arm64/branch_index_read_arm64_test.go src/core/compiler/backend/railshot/arm64/control.go src/core/compiler/backend/railshot/arm64/knobs.go src/core/compiler/optimization/catalog.go
go test -tags wago_codegenstats,wago_regalloccheck,wago_guardpage ./src/core/compiler/backend/railshot/arm64 -run '^Test(BranchIndexRead|BoundedBranchIndex)' -count=1 > "$root/branch-index-read-strict.txt" 2>&1
go build -tags wago_guardpage -o /tmp/paired-branch-index-read ./experiments/arm64-parity/paired
for phase in exec compile; do
    /tmp/paired-branch-index-read -option branch-index-read -workloads language-register-vm,compiler-register-allocation,audio-adpcm,vision-dilation -phase "$phase" -rounds 8 -budget 150ms -code-dir /tmp/branch-index-read-images > "$root/branch-index-read-screen-$phase.jsonl" 2>&1
done
python3 "$root/summarize_paired.py" "$root/branch-index-read-screen-exec.jsonl" "$root/branch-index-read-screen-compile.jsonl" > "$root/branch-index-read-screen-summary.md"
python3 - <<'PY'
from pathlib import Path
r=Path('/tmp/branch-index-read-images');lines=[]
for name in ['language-register-vm','compiler-register-allocation','audio-adpcm','vision-dilation']:
    old=(r/(name+'-0.bin')).read_bytes();new=(r/(name+'-1.bin')).read_bytes()
    lines.append(f'{name}: {len(old)}B -> {len(new)}B; byte-identical={old==new}')
baseline=Path('/tmp/bounded-branch-index-images/language-register-vm-1.bin').read_bytes()
assert baseline==(r/'language-register-vm-0.bin').read_bytes()
Path('/Users/work/Code/Wago/wago/experiments/arm64-parity/branch-index-read-screen-native-proof.txt').write_text('\n'.join(lines)+'\nOFF VM matches the accepted bounded-index image.\n')
PY
