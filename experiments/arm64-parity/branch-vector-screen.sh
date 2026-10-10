#!/bin/sh
set -eu
export WAGO_SHARED_SCALAR=0 WAGO_ARM64_EXPERIMENT_BR_TABLE_BRANCH_VECTOR=1
export WAGO_PARITY_CACHE=/Users/work/Code/Web/wasm.fyi/.wasmbench/corpus-cache WAGO_PARITY_CORPUS=/Users/work/Code/Web/wasm.fyi/corpora/applications
clang --target=aarch64-linux-gnu -c experiments/arm64-parity/branch-vector-prototype/encoding.s -o /tmp/branch-vector-encoding.o
python3 - <<'PY'
from pathlib import Path
import struct
b=Path('/tmp/branch-vector-encoding.o').read_bytes();h=struct.unpack_from('<16sHHIQQQIHHHHHH',b)
sections=[struct.unpack_from('<IIQQQQIIQQ',b,h[6]+i*h[11]) for i in range(h[12])]
s=sections[h[13]];names=b[s[4]:s[4]+s[5]]
text=next(s for s in sections if names[s[0]:].split(b'\0',1)[0]==b'.text')
code=b[text[4]:text[4]+text[5]];words=struct.unpack('<4I',code)
assert words==(0x8b224020,0x8b224820,0x8b225020,0x8b294a11),words
Path('experiments/arm64-parity/branch-vector-encoding-proof.txt').write_text('Clang independent goldens pass: '+', '.join(hex(w) for w in words)+'\n')
PY
go test -tags wago_codegenstats,wago_regalloccheck,wago_guardpage ./src/core/compiler/backend/railshot/arm64 ./src/core/compiler/optimization ./src/core/encoder/arm64 > experiments/arm64-parity/branch-vector-tests.txt 2>&1
go test -tags wago_guardpage -c -o /tmp/parity-branch-vector.test ./experiments/arm64-parity
WAGO_PARITY_CORE_CODE_DIR=/tmp/branch-vector-core /tmp/parity-branch-vector.test -test.run='^TestCachedCoreOracles$' > experiments/arm64-parity/branch-vector-core.txt 2>&1
WAGO_PARITY_CODE_DIR=/tmp/branch-vector-app /tmp/parity-branch-vector.test -test.run=^$ -test.bench='BenchmarkParity/.*/Exec$' -test.benchtime=1x > experiments/arm64-parity/branch-vector-app.txt 2>&1
python3 - <<'PY'
from pathlib import Path
out=[]
for kind,count in [('core',46),('app',102)]:
 files=list(Path('/tmp/branch-vector-'+kind).glob('*.bin'));assert len(files)==count
 for p in sorted(files):
  old=Path('/tmp/borrowed-div-rem-retained-'+kind)/p.name
  if p.read_bytes()!=old.read_bytes():out.append(f'{kind} {p.name} {old.stat().st_size} {p.stat().st_size}')
Path('experiments/arm64-parity/branch-vector-native-changes.txt').write_text('\n'.join(out)+'\n')
PY
