#!/bin/sh
set -eu
export WAGO_SHARED_SCALAR=0
export WAGO_ARM64_EXPERIMENT_BOUNDED_BRANCH_INDEX=1
root=/Users/work/Code/Wago/wago/experiments/arm64-parity
cd /tmp/wago-bounded-dispatch-37174169
go build -tags wago_runtime,wago_profile,wago_guardpage -o /tmp/wago-bounded-branch-index-profile ./cli/wago
python3 - <<'PY'
import base64,json,subprocess
from pathlib import Path
base=Path('/Users/work/Code/Web/wasm.fyi/corpora/applications')
root=Path('/Users/work/Code/Wago/wago/experiments/arm64-parity')
name='language-register-vm'
w=next(w for w in json.loads((base/'manifest.json').read_text()) if w['id']=='applications/'+name)
prefix='profile-bounded-branch-index-on'
with (root/(prefix+'.txt')).open('w') as log:
    subprocess.run(['/tmp/wago-bounded-branch-index-profile','profile','record','--module',str(base/w['artifact']),
        '--export',w['export'],'--args',','.join(map(str,w['args'])),'--want',','.join(map(str,w['oracle']['expected'])),
        '--bounds','signals','--mode','prepared','--duration','3s','--backend','samply','--samply','/opt/homebrew/bin/samply',
        '--rate','1000','--include-code','--source-maps','--out',str(root/prefix)],check=True,stdout=log,stderr=subprocess.STDOUT)
images=[r['image'] for r in json.loads((root/prefix/'images.json').read_text()) if r.get('image')]
assert len(images)==1
code=base64.b64decode(images[0]['code'])
assert code==(Path('/tmp/bounded-branch-index-on-app')/(name+'.bin')).read_bytes()
(root/'bounded-branch-index-profile-native-proof.txt').write_text('Exact register-VM oracle passes; embedded1224-byte image matches the qualified/timed candidate.\n')
with (root/(prefix+'-assembly.txt')).open('w') as log:
    subprocess.run(['/tmp/wago-profile-tools/bin/python',str(root/'sampled_assembly.py'),str(root/prefix)],check=True,stdout=log)
PY
