#!/bin/sh
set -eu
export WAGO_SHARED_SCALAR=0
export WAGO_PARITY_CACHE=/Users/work/Code/Web/wasm.fyi/.wasmbench/corpus-cache
export WAGO_PARITY_CORPUS=/Users/work/Code/Web/wasm.fyi/corpora/applications
root=/Users/work/Code/Wago/wago/experiments/arm64-parity
cd /tmp/wago-taken-branch-37174169
go test -c -tags wago_guardpage -o /tmp/parity-taken-zero-branch-signal.test ./experiments/arm64-parity
go test -c -o /tmp/parity-taken-zero-branch-explicit.test ./experiments/arm64-parity
for state in off on; do
 if [ "$state" = on ]; then export WAGO_ARM64_EXPERIMENT_TAKEN_ZERO_BRANCH=1; else export WAGO_ARM64_EXPERIMENT_TAKEN_ZERO_BRANCH=0; fi
 WAGO_PARITY_CORE_CODE_DIR=/tmp/taken-zero-branch-$state-core /tmp/parity-taken-zero-branch-signal.test -test.run '^TestCachedCoreOracles$' -test.v > "$root/taken-zero-branch-$state-signal-core.txt" 2>&1
 WAGO_PARITY_CODE_DIR=/tmp/taken-zero-branch-$state-app /tmp/parity-taken-zero-branch-signal.test -test.run '^$' -test.bench '^BenchmarkParity/.*/Exec$' -test.benchtime=1x -test.count=1 > "$root/taken-zero-branch-$state-signal-app.txt" 2>&1
done
export WAGO_ARM64_EXPERIMENT_TAKEN_ZERO_BRANCH=1
/tmp/parity-taken-zero-branch-explicit.test -test.run '^TestCachedCoreOracles$' -test.v > "$root/taken-zero-branch-on-explicit-core.txt" 2>&1
/tmp/parity-taken-zero-branch-explicit.test -test.run '^$' -test.bench '^BenchmarkParity/.*/Exec$' -test.benchtime=1x -test.count=1 > "$root/taken-zero-branch-on-explicit-app.txt" 2>&1
python3 - <<'PY'
from pathlib import Path
import struct
out=Path('/Users/work/Code/Wago/wago/experiments/arm64-parity/taken-zero-branch-native-proof.txt')
lines=[]; total=0
for kind,expected in [('core',46),('app',102)]:
 off=Path('/tmp/taken-zero-branch-off-'+kind);on=Path('/tmp/taken-zero-branch-on-'+kind);base=Path('/tmp/hour-20261011-'+kind)
 files=sorted(off.glob('*.bin'))
 assert len(files)==expected,(kind,len(files))
 assert {p.name for p in files}=={p.name for p in on.glob('*.bin')}
 for p in files:
  old=p.read_bytes();new=(on/p.name).read_bytes()
  assert old==(base/p.name).read_bytes(),p.name
  assert len(old)==len(new),p.name
  total+=1
  if old==new:continue
  differences=[]
  for pc in range(0,len(old),4):
   w,w2=(struct.unpack_from('<I',b,pc)[0] for b in (old,new))
   if w==w2:continue
   assert w&0x7e000000==0x34000000 and (w^w2)&0xff00001f==0,(p.name,pc,hex(w),hex(w2))
   differences.append(f'+0x{pc:x}: {w:08x}->{w2:08x}')
  lines.append(f'{p.name}: {len(old)}B unchanged size; {len(differences)} direct zero branch retargets; '+', '.join(differences))
lines.append(f'{total} exact SIGNAL contracts and native images inspected; all disabled images byte-identical to37174169. Explicit exact outputs also pass.')
out.write_text('\n'.join(lines)+'\n')
print(out.read_text())
PY
