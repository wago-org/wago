#!/bin/sh
set -eu
export WAGO_SHARED_SCALAR=0
go build -tags wago_runtime,wago_profile,wago_guardpage -o /tmp/wago-next-20261011-profile ./cli/wago
go build -tags wago_guardpage -o /tmp/paired-next-20261011-base ./experiments/arm64-parity/paired
python3 - <<'PROFILE'
from pathlib import Path
import base64,json,os,subprocess
base=Path('/Users/work/Code/Web/wasm.fyi/corpora/applications')
root=Path('experiments/arm64-parity').resolve()
proof=[]
for name in ['geo-point-in-polygon','compiler-register-allocation','language-register-vm']:
 w=next(w for w in json.loads((base/'manifest.json').read_text()) if w['id']=='applications/'+name)
 prefix='profile-next-20261011-'+name
 with (root/(prefix+'.txt')).open('w') as out:
  subprocess.run(['/tmp/wago-next-20261011-profile','profile','record','--module',str(base/w['artifact']),'--export',w['export'],'--args',','.join(map(str,w['args'])),'--want',','.join(map(str,w['oracle']['expected'])),'--bounds','signals','--mode','prepared','--duration','3s','--backend','samply','--samply','/opt/homebrew/bin/samply','--rate','1000','--include-code','--source-maps','--out',str(root/prefix)],stdout=out,stderr=subprocess.STDOUT,check=True)
 images=[e['image'] for e in json.loads((root/prefix/'images.json').read_text()) if e.get('image')]
 assert len(images)==1
 code=base64.b64decode(images[0]['code'])
 assert code==(Path('/tmp/hour-20261011-app')/(name+'.bin')).read_bytes(),name
 proof.append(f'{name}: exact oracle passes; {len(code)}-byte profile image matches current qualified baseline.')
 with (root/(prefix+'-assembly.txt')).open('w') as out:
  subprocess.run(['/tmp/wago-profile-tools/bin/python',str(root/'sampled_assembly.py'),str(root/prefix)],stdout=out,check=True)
(root/'next-20261011-baseline-profile-proof.txt').write_text('\n'.join(proof)+'\n')
PROFILE
