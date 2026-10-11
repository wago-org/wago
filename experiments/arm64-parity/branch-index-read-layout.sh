#!/bin/sh
set -eu
export WAGO_SHARED_SCALAR=0
unset WAGO_ARM64_NO_BOUNDED_BRANCH_INDEX
root=/Users/work/Code/Wago/wago/experiments/arm64-parity
cd /tmp/wago-branch-index-read-37174169
gofmt -w src/core/compiler/backend/railshot/arm64/branch_index_read.go
go test -tags wago_codegenstats,wago_regalloccheck,wago_guardpage ./src/core/compiler/backend/railshot/arm64 -run '^Test(BranchIndexRead|BoundedBranchIndex)' -count=1 > "$root/branch-index-read-layout-strict.txt" 2>&1
go build -tags wago_guardpage -o /tmp/paired-branch-index-read-layout ./experiments/arm64-parity/paired
for phase in exec compile; do
    /tmp/paired-branch-index-read-layout -option branch-index-read -workloads language-register-vm -phase "$phase" -rounds 10 -budget 250ms -code-dir /tmp/branch-index-read-layout-images > "$root/branch-index-read-layout-$phase.jsonl" 2>&1
done
python3 "$root/summarize_paired.py" "$root/branch-index-read-layout-exec.jsonl" "$root/branch-index-read-layout-compile.jsonl" > "$root/branch-index-read-layout-summary.md"
python3 - <<'PY'
from pathlib import Path
import struct
r=Path('/tmp/branch-index-read-layout-images')
old=(r/'language-register-vm-0.bin').read_bytes();new=(r/'language-register-vm-1.bin').read_bytes()
assert old==Path('/tmp/bounded-branch-index-images/language-register-vm-1.bin').read_bytes()
assert len(old)==len(new)==1224
changes=[]
for pc in range(0,len(old),4):
    a,b=(struct.unpack_from('<I',code,pc)[0] for code in (old,new))
    if a!=b:changes.append(f'+0x{pc:x}: {a:08x}->{b:08x}')
assert len(changes)==2,changes
Path('/Users/work/Code/Wago/wago/experiments/arm64-parity/branch-index-read-layout-native-proof.txt').write_text('VM1224B unchanged layout;exactly two changed words: '+', '.join(changes)+'\nOFF image matches accepted bounded-index baseline.\n')
PY
