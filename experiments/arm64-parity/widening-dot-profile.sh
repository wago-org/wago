#!/bin/sh
set -eu
export WAGO_SHARED_SCALAR=0
export WAGO_ARM64_EXPERIMENT_WIDENING_DOT=1
root=/Users/work/Code/Wago/wago/experiments/arm64-parity
cd /tmp/wago-widening-dot-37174169
go build -tags wago_runtime,wago_profile,wago_guardpage -o /tmp/wago-widening-dot-profile ./cli/wago
python3 - <<'PY'
import base64,json,subprocess
from pathlib import Path
cache=Path('/Users/work/Code/Web/wasm.fyi/.wasmbench/corpus-cache')
root=Path('/Users/work/Code/Wago/wago/experiments/arm64-parity')
w=next(x for x in json.loads((cache/'manifest.json').read_text())['workloads'] if x['id']=='wago/libtommath/modular-arithmetic')
out=root/'profile-widening-dot-on'
with (root/'profile-widening-dot-on.txt').open('w') as log:
 subprocess.run(['/tmp/wago-widening-dot-profile','profile','record','--module',str(cache/w['artifact']),'--export',w['export'],'--args',','.join(map(str,w['args'])),'--want',','.join(map(str,w['oracle']['expected'])),'--bounds','signals','--mode','prepared','--duration','3s','--backend','samply','--samply','/opt/homebrew/bin/samply','--rate','1000','--include-code','--source-maps','--out',str(out)],stdout=log,stderr=subprocess.STDOUT,check=True)
images=[e['image'] for e in json.loads((out/'images.json').read_text()) if e.get('image')]
assert len(images)==1
code=base64.b64decode(images[0]['code'])
assert code==(Path('/tmp/widening-dot-images')/'modular-arithmetic-1.bin').read_bytes()
(root/'widening-dot-profile-native-proof.txt').write_text(f'Exact LibTomMath oracle passes; {len(code)}-byte profile image matches timed candidate.\n')
with (root/'profile-widening-dot-on-assembly.txt').open('w') as log:
 subprocess.run(['/tmp/wago-profile-tools/bin/python',str(root/'sampled_assembly.py'),str(out)],stdout=log,check=True)
PY
