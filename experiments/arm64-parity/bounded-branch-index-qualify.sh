#!/bin/sh
set -eu
export WAGO_SHARED_SCALAR=0
export WAGO_PARITY_CACHE=/Users/work/Code/Web/wasm.fyi/.wasmbench/corpus-cache
export WAGO_PARITY_CORPUS=/Users/work/Code/Web/wasm.fyi/corpora/applications
export WAGO_ARM64_EXPERIMENT_BOUNDED_BRANCH_INDEX=1
root=/Users/work/Code/Wago/wago/experiments/arm64-parity
cd /tmp/wago-bounded-dispatch-37174169
gofmt -w src/core/compiler/backend/railshot/arm64/bounded_branch_index_arm64_test.go
go test -tags wago_codegenstats,wago_regalloccheck,wago_guardpage ./src/core/compiler/backend/railshot/arm64 ./src/core/compiler/optimization ./src/core/encoder/arm64 ./src/core/runtime > "$root/bounded-branch-index-diagnostics.txt" 2>&1
go test ./src/core/compiler/backend/railshot/arm64 -run '^TestBoundedBranchIndex' -count=1 > "$root/bounded-branch-index-plain.txt" 2>&1
go test -c -tags wago_guardpage -o /tmp/parity-bounded-branch-index-signal.test ./experiments/arm64-parity
go test -c -o /tmp/parity-bounded-branch-index-explicit.test ./experiments/arm64-parity
WAGO_PARITY_CORE_CODE_DIR=/tmp/bounded-branch-index-on-core /tmp/parity-bounded-branch-index-signal.test -test.run '^TestCachedCoreOracles$' -test.v > "$root/bounded-branch-index-on-signal-core.txt" 2>&1
WAGO_PARITY_CODE_DIR=/tmp/bounded-branch-index-on-app /tmp/parity-bounded-branch-index-signal.test -test.run '^$' -test.bench '^BenchmarkParity/.*/Exec$' -test.benchtime=1x -test.count=1 > "$root/bounded-branch-index-on-signal-app.txt" 2>&1
/tmp/parity-bounded-branch-index-explicit.test -test.run '^TestCachedCoreOracles$' -test.v > "$root/bounded-branch-index-on-explicit-core.txt" 2>&1
/tmp/parity-bounded-branch-index-explicit.test -test.run '^$' -test.bench '^BenchmarkParity/.*/Exec$' -test.benchtime=1x -test.count=1 > "$root/bounded-branch-index-on-explicit-app.txt" 2>&1
python3 - <<'PY'
from pathlib import Path
import struct
root=Path('/Users/work/Code/Wago/wago/experiments/arm64-parity')
lines=[];count=0;changed=[]
for kind,expected in [('core',46),('app',102)]:
    base=Path('/tmp/hour-20261011-'+kind)
    on=Path('/tmp/bounded-branch-index-on-'+kind)
    files=sorted(on.glob('*.bin'))
    assert len(files)==expected,(kind,len(files))
    for p in files:
        old=(base/p.name).read_bytes();new=p.read_bytes();count+=1
        if old==new:continue
        changed.append(p.name)
        lines.append(f'{p.name}: {len(old)}B -> {len(new)}B')
assert changed==['language-register-vm.bin'],changed
screen=Path('/tmp/bounded-branch-index-images/language-register-vm-1.bin').read_bytes()
assert screen==(Path('/tmp/bounded-branch-index-on-app/language-register-vm.bin')).read_bytes()
lines.append(f'{count} exact SIGNAL contracts/images inspected;147 unchanged against37174169;only VM changes and matches frozen timed candidate. All148 explicit exact contracts also pass.1x application records are correctness/image capture only.')
(root/'bounded-branch-index-native-proof.txt').write_text('\n'.join(lines)+'\n')
print('\n'.join(lines))
PY
