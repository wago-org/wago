#!/bin/sh
set -eu
export WAGO_SHARED_SCALAR=0
unset WAGO_ARM64_EXPERIMENT_BORROWED_DIV_REM WAGO_ARM64_NO_BORROWED_DIV_REM
go build -tags wago_runtime,wago_profile,wago_guardpage -o /tmp/wago-borrowed-div-rem-retained-profile ./cli/wago
python3 - <<'PY'
from pathlib import Path
import json,subprocess
base=Path('/Users/work/Code/Web/wasm.fyi/corpora/applications')
manifest=json.loads((base/'manifest.json').read_text())
root=Path('/Users/work/Code/Wago/wago/experiments/arm64-parity')
for name in ['language-register-vm','geo-point-in-polygon']:
 w=next(w for w in manifest if w['id']=='applications/'+name)
 assert len(w['args'])==1 and w['oracle']['kind']=='exact_u64'
 prefix='profile-borrowed-div-rem-retained-'+name
 with (root/(prefix+'.txt')).open('w') as out:
  subprocess.run(['/tmp/wago-borrowed-div-rem-retained-profile','profile','record','--module',str(base/w['artifact']),'--export',w['export'],'--args',str(w['args'][0]),'--want',w['oracle']['expected'][0],'--bounds','signals','--mode','prepared','--duration','3s','--backend','samply','--samply','/opt/homebrew/bin/samply','--rate','1000','--include-code','--source-maps','--out',str(root/prefix)],check=True,stdout=out,stderr=subprocess.STDOUT)
 with (root/(prefix+'-assembly.txt')).open('w') as out:
  subprocess.run(['/tmp/wago-profile-tools/bin/python',str(root/'sampled_assembly.py'),str(root/prefix)],check=True,stdout=out)
PY
