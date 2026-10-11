#!/bin/sh
set -eu
export WAGO_SHARED_SCALAR=0
export WAGO_ARM64_EXPERIMENT_TAKEN_ZERO_BRANCH=1
root=/Users/work/Code/Wago/wago/experiments/arm64-parity
cd /tmp/wago-taken-branch-37174169
go build -tags wago_runtime,wago_profile,wago_guardpage -o /tmp/wago-taken-zero-branch-mitigated-profile ./cli/wago
python3 - <<'PY'
import base64,json,subprocess
from pathlib import Path
base=Path('/Users/work/Code/Web/wasm.fyi/corpora/applications')
root=Path('/Users/work/Code/Wago/wago/experiments/arm64-parity')
name='hardware-boolean-anf'
w=next(w for w in json.loads((base/'manifest.json').read_text()) if w['id']=='applications/'+name)
prefix='profile-taken-zero-branch-on'
with (root/(prefix+'.txt')).open('w') as log:
 subprocess.run(['/tmp/wago-taken-zero-branch-mitigated-profile','profile','record','--module',str(base/w['artifact']),'--export',w['export'],'--args',','.join(map(str,w['args'])),'--want',','.join(map(str,w['oracle']['expected'])),'--bounds','signals','--mode','prepared','--duration','3s','--backend','samply','--samply','/opt/homebrew/bin/samply','--rate','1000','--include-code','--source-maps','--out',str(root/prefix)],check=True,stdout=log,stderr=subprocess.STDOUT)
images=[r['image'] for r in json.loads((root/prefix/'images.json').read_text()) if r.get('image')]
assert len(images)==1
code=base64.b64decode(images[0]['code'])
assert code==(Path('/tmp/taken-zero-branch-on-app')/(name+'.bin')).read_bytes()
(root/'taken-zero-branch-profile-native-proof.txt').write_text('Exact Boolean ANF oracle passes; embedded1904-byte image matches qualified/timed candidate.\n')
with (root/(prefix+'-assembly.txt')).open('w') as log:
 subprocess.run(['/tmp/wago-profile-tools/bin/python',str(root/'sampled_assembly.py'),str(root/prefix)],check=True,stdout=log)
PY
