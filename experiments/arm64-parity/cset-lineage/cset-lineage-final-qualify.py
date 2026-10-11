import os,subprocess,json,base64
from pathlib import Path
out=Path('/tmp/cset-lineage-final');out.mkdir(exist_ok=True)
cwd='/tmp/wago-cset-lineage-76e52be8'
e=os.environ.copy();e.update(GOWORK='off',WAGO_SHARED_SCALAR='0',WAGO_ARM64_EXPERIMENT_CSET_LINEAGE='1',WAGO_PARITY_CACHE='/Users/work/Code/Web/wasm.fyi/.wasmbench/corpus-cache',WAGO_PARITY_CORPUS='/Users/work/Code/Web/wasm.fyi/corpora/applications')
for label,tags,packages in [('plain','', ['./src/core/compiler/backend/railshot/arm64']),('diagnostic','wago_guardpage,wago_codegenstats,wago_regalloccheck',['./src/core/compiler/backend/railshot/arm64','./src/core/compiler/optimization','./src/core/encoder/arm64','./src/core/runtime/arm64spike'])]:
 cmd=['go','test']+(['-tags',tags] if tags else [])+packages+['-count=1']
 with (out/(label+'-tests.txt')).open('w') as f:subprocess.run(cmd,cwd=cwd,env=e,stdout=f,stderr=subprocess.STDOUT,check=True)
 print(label+' tests PASS',flush=True)
for mode,tags in [('signal','wago_guardpage'),('explicit','')]:
 exe='/tmp/parity-cset-lineage-final-'+mode+'.test'
 cmd=['go','test','-c']+(['-tags',tags] if tags else [])+['-o',exe,'./experiments/arm64-parity']
 subprocess.run(cmd,cwd=cwd,env=e,check=True)
 for on in (True,False):
  ee=e|dict(WAGO_ARM64_EXPERIMENT_CSET_LINEAGE=str(int(on)))
  for kind,cmd,extra in [('core',['-test.run','^TestCachedCoreOracles$','-test.v'],dict(WAGO_PARITY_CORE_CODE_DIR=str(out/(mode+'-'+str(int(on))+'-core')))),('app',['-test.run','^$','-test.bench','^BenchmarkParity/.*/Exec$','-test.benchtime=1x','-test.count=1'],dict(WAGO_PARITY_CODE_DIR=str(out/(mode+'-'+str(int(on))+'-app'))))]:
   with (out/(mode+'-'+str(int(on))+'-'+kind+'.txt')).open('w') as f:subprocess.run([exe]+cmd,env=ee|extra,stdout=f,stderr=subprocess.STDOUT,check=True)
 print(mode+' ON/OFF all148 exact PASS',flush=True)
proof=[]
for kind in ('core','app'):
 first=Path('/tmp/cset-lineage-inventory/'+kind);base=Path('/tmp/next-20261011-select-on-'+kind)
 for p in first.glob('*.bin'):
  assert p.read_bytes()==(out/('signal-1-'+kind)/p.name).read_bytes()
  assert (base/p.name).read_bytes()==(out/('signal-0-'+kind)/p.name).read_bytes()
 proof.append(kind+': all finalSIGNAL ON images exact first qualified/timedcandidate; all finalOFF exact pristine-source root3 baseline')
(out/'native-proof.txt').write_text('\n'.join(proof)+'\n')
print('All148 SIGNAL finalON/OFF native image identity PASS',flush=True)
subprocess.run(['go','build','-tags','wago_runtime,wago_profile,wago_guardpage','-o','/tmp/wago-cset-lineage-profile','./cli/wago'],cwd=cwd,env=e,check=True)
base=Path(e['WAGO_PARITY_CORPUS']);w=next(w for w in json.loads((base/'manifest.json').read_text()) if w['id']=='applications/ml-kmeans')
with (out/'profile.txt').open('w') as f:subprocess.run(['/tmp/wago-cset-lineage-profile','profile','record','--module',str(base/w['artifact']),'--export',w['export'],'--args',','.join(map(str,w['args'])),'--want',','.join(map(str,w['oracle']['expected'])),'--bounds','signals','--mode','prepared','--duration','3s','--backend','samply','--samply','/opt/homebrew/bin/samply','--rate','1000','--include-code','--source-maps','--out',str(out/'profile')],cwd=cwd,env=e,stdout=f,stderr=subprocess.STDOUT,check=True)
images=[x['image'] for x in json.loads((out/'profile/images.json').read_text()) if x.get('image')];assert len(images)==1
code=base64.b64decode(images[0]['code']);assert code==(out/'signal-1-app/ml-kmeans.bin').read_bytes()
(out/'profile-proof.txt').write_text(f'Exact1342930999 oracle passes; {len(code)}B profile image byte-identical to qualified/timed ML.\n')
with (out/'profile-assembly.txt').open('w') as f:subprocess.run(['/tmp/wago-profile-tools/bin/python','/Users/work/Code/Wago/wago/experiments/arm64-parity/sampled_assembly.py',str(out/'profile')],stdout=f,check=True)
print('Profile exactoracle/nativeidentity PASS',flush=True)
