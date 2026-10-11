#!/bin/sh
set -eu
export WAGO_SHARED_SCALAR=0
go test -tags wago_codegenstats,wago_regalloccheck,wago_guardpage ./src/core/compiler/backend/railshot/arm64 -run 'TestOrdinaryShiftedImmediateSourceLiveAdmission|TestBoundedSignedQuotient' -count=1 > experiments/arm64-parity/hour-20261011-final-fixtures.txt 2>&1
go test ./src/core/compiler/backend/railshot/arm64 > experiments/arm64-parity/hour-20261011-final-plain-tests.txt 2>&1
go build -tags wago_runtime,wago_profile,wago_guardpage -o /tmp/wago-hour-20261011-profile ./cli/wago
python3 - <<'PROFILE'
from pathlib import Path
import base64,json,os,subprocess
base=Path('/Users/work/Code/Web/wasm.fyi/corpora/applications')
root=Path('experiments/arm64-parity').resolve()
w=next(w for w in json.loads((base/'manifest.json').read_text()) if w['id']=='applications/files-glob-match')
features=['REGIONAL_MEMORY_READ','DOMINATED_INDEXED_KEY','BOUNDED_SIGNED_QUOTIENT','WIDE_REUSE_PINS','ORDINARY_SHIFTED_IMMEDIATE']
proof=[]
for enabled in [False,True]:
 prefix='profile-hour-20261011-glob-'+('on' if enabled else 'off')
 env=dict(os.environ)
 for feature in features:
  env.pop('WAGO_ARM64_NO_'+feature,None)
  env['WAGO_ARM64_EXPERIMENT_'+feature]=str(int(enabled))
 with (root/(prefix+'.txt')).open('w') as out:
  subprocess.run(['/tmp/wago-hour-20261011-profile','profile','record','--module',str(base/w['artifact']),'--export',w['export'],'--args',str(w['args'][0]),'--want',w['oracle']['expected'][0],'--bounds','signals','--mode','prepared','--duration','3s','--backend','samply','--samply','/opt/homebrew/bin/samply','--rate','1000','--include-code','--source-maps','--out',str(root/prefix)],env=env,stdout=out,stderr=subprocess.STDOUT,check=True)
 images=[e['image'] for e in json.loads((root/prefix/'images.json').read_text()) if e.get('image')]
 assert len(images)==1
 code=base64.b64decode(images[0]['code'])
 measured=Path('/tmp/hour-20261011-focused-images')/('files-glob-match-'+str(int(enabled))+'.bin')
 assert code==measured.read_bytes(),('profile image differs from paired image',enabled)
 proof.append(f'{prefix}: exact oracle passes; {len(code)}-byte image equals integrated paired state {int(enabled)}.')
 with (root/(prefix+'-assembly.txt')).open('w') as out:
  subprocess.run(['/tmp/wago-profile-tools/bin/python',str(root/'sampled_assembly.py'),str(root/prefix)],stdout=out,check=True)
(root/'hour-20261011-profile-native-proof.txt').write_text('\n'.join(proof)+'\n')
PROFILE
