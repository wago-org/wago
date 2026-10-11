#!/bin/sh
set -eu
export WAGO_SHARED_SCALAR=0
export WAGO_PARITY_CACHE=/Users/work/Code/Web/wasm.fyi/.wasmbench/corpus-cache
export WAGO_PARITY_CORPUS=/Users/work/Code/Web/wasm.fyi/corpora/applications
unset WAGO_ARM64_NO_GUARDED_BOOLEAN_XOR WAGO_ARM64_NO_TAKEN_ZERO_BRANCH
root=experiments/arm64-parity
go test ./src/core/compiler/backend/railshot/arm64 ./src/core/compiler/optimization ./src/core/encoder/arm64 > "$root/next-20261011-retain-plain.txt" 2>&1
go test -tags wago_codegenstats,wago_regalloccheck,wago_guardpage ./src/core/compiler/backend/railshot/arm64 ./src/core/compiler/optimization ./src/core/encoder/arm64 ./src/core/runtime > "$root/next-20261011-retain-diagnostics.txt" 2>&1
go test -c -tags wago_guardpage -o /tmp/parity-next-20261011-retained-signal.test ./experiments/arm64-parity
go test -c -o /tmp/parity-next-20261011-retained-explicit.test ./experiments/arm64-parity
go build -tags wago_guardpage -o /tmp/paired-next-20261011-retained ./experiments/arm64-parity/paired
for state in on off; do
 if [ "$state" = off ]; then export WAGO_ARM64_NO_GUARDED_BOOLEAN_XOR=1 WAGO_ARM64_NO_TAKEN_ZERO_BRANCH=1; fi
 WAGO_PARITY_CORE_CODE_DIR=/tmp/next-20261011-$state-core /tmp/parity-next-20261011-retained-signal.test -test.run '^TestCachedCoreOracles$' -test.v > "$root/next-20261011-$state-signal-core.txt" 2>&1
 WAGO_PARITY_CODE_DIR=/tmp/next-20261011-$state-app /tmp/parity-next-20261011-retained-signal.test -test.run '^$' -test.bench '^BenchmarkParity/.*/Exec$' -test.benchtime=1x -test.count=1 > "$root/next-20261011-$state-signal-app.txt" 2>&1
done
unset WAGO_ARM64_NO_GUARDED_BOOLEAN_XOR WAGO_ARM64_NO_TAKEN_ZERO_BRANCH
/tmp/parity-next-20261011-retained-explicit.test -test.run '^TestCachedCoreOracles$' -test.v > "$root/next-20261011-on-explicit-core.txt" 2>&1
/tmp/parity-next-20261011-retained-explicit.test -test.run '^$' -test.bench '^BenchmarkParity/.*/Exec$' -test.benchtime=1x -test.count=1 > "$root/next-20261011-on-explicit-app.txt" 2>&1
python3 - <<'PY'
from pathlib import Path
import base64,json
root=Path('experiments/arm64-parity')
images=[r['image'] for r in json.loads((root/'guarded-boolean-xor/profile-on/images.json').read_text()) if r.get('image')]
assert len(images)==1
polygon=base64.b64decode(images[0]['code'])
lines=[];total=0;changed=[]
for kind,expected in [('core',46),('app',102)]:
 on=Path('/tmp/next-20261011-on-'+kind);off=Path('/tmp/next-20261011-off-'+kind);base=Path('/tmp/hour-20261011-'+kind)
 files=list(on.glob('*.bin'));assert len(files)==expected
 assert {p.name for p in files}=={p.name for p in off.glob('*.bin')}
 for p in files:
  old=(base/p.name).read_bytes();actual=p.read_bytes()
  assert (off/p.name).read_bytes()==old,p.name
  wanted=old
  if p.name=='geo-point-in-polygon.bin':wanted=polygon
  elif p.name=='hardware-boolean-anf.bin':wanted=(Path('/tmp/taken-zero-branch-on-app')/p.name).read_bytes()
  assert actual==wanted,p.name
  total+=1
  if actual!=old:changed.append(p.name)
lines.append(f'{total} combined default-ON SIGNAL images match independently timed and qualified changes; only '+', '.join(changed)+' change.')
lines.append(f'{total} both-OFF SIGNAL images are byte-identical to exact37174169. All exact contracts pass in both bounds modes;1x application timings are used only for correctness/image capture.')
(root/'next-20261011-retain-native-proof.txt').write_text('\n'.join(lines)+'\n')
print('\n'.join(lines))
PY
