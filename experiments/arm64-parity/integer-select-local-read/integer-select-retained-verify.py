import os,subprocess,json,base64
from pathlib import Path
out=Path('/tmp/integer-select-narrow');cwd='/tmp/wago-integer-select-read-37174169';e=os.environ.copy();e.update(GOWORK='off',WAGO_SHARED_SCALAR='0',WAGO_PARITY_CACHE='/Users/work/Code/Web/wasm.fyi/.wasmbench/corpus-cache',WAGO_PARITY_CORPUS='/Users/work/Code/Web/wasm.fyi/corpora/applications')
for diag,args in [('retained-diagnostics',['-tags','wago_guardpage,wago_codegenstats,wago_regalloccheck','./src/core/compiler/backend/railshot/arm64','./src/core/compiler/optimization','./src/core/encoder/arm64']),('retained-plain',['./src/core/compiler/backend/railshot/arm64','-run','^TestIntegerSelectLocalRead','-count=1'])]:
 with (out/(diag+'.txt')).open('w') as f:subprocess.run(['go','test']+args,cwd=cwd,env=e,stdout=f,stderr=subprocess.STDOUT,check=True)
proof=[]
for mode in ('signal','explicit'):
 binary='/tmp/parity-integer-select-local-retained-'+mode+'.test';cmd=['go','test','-c','-o',binary]
 if mode=='signal':cmd+=['-tags','wago_guardpage']
 subprocess.run(cmd+['./experiments/arm64-parity'],cwd=cwd,env=e,check=True)
 for on in ([True,False] if mode=='signal' else [True]):
  for kind,args,key in [('core',['-test.run','^TestCachedCoreOracles$','-test.v'],'WAGO_PARITY_CORE_CODE_DIR'),('app',['-test.run','^$','-test.bench','^BenchmarkParity/.*/Exec$','-test.benchtime=1x','-test.count=1'],'WAGO_PARITY_CODE_DIR')]:
   folder='/tmp/integer-select-retained-'+mode+'-'+str(int(on))+'-'+kind
   ee=e|{key:folder,'WAGO_ARM64_NO_INTEGER_SELECT_LOCAL_READ':str(int(not on))}
   with (out/('retained-'+mode+'-'+str(int(on))+'-'+kind+'.txt')).open('w') as f:subprocess.run([binary]+args,env=ee,stdout=f,stderr=subprocess.STDOUT,check=True)
   base=Path('/tmp/'+('integer-select-narrow-' if mode=='signal' else 'integer-select-narrow-explicit-')+kind) if on else Path('/tmp/hour-20261011-'+kind)
   ps=list(Path(folder).glob('*.bin'));assert len(ps)==(46 if kind=='core' else 102)
   for p in ps:assert p.read_bytes()==(base/p.name).read_bytes(),p.name
   proof.append(f'{mode} {"default ON" if on else "disabled"}: {len(ps)}{kind} exact contracts pass; images match '+('qualified narrow ON.' if on else 'pristine37174169.'))
(out/'retained-native-proof.txt').write_text('\n'.join(proof)+'\n')
subprocess.run(['go','build','-tags','wago_runtime,wago_profile,wago_guardpage','-o','/tmp/wago-integer-select-local-retained-profile','./cli/wago'],cwd=cwd,env=e,check=True)
base=Path(e['WAGO_PARITY_CORPUS']);w=next(w for w in json.loads((base/'manifest.json').read_text()) if w['id']=='applications/ml-kmeans')
with (out/'retained-profile.txt').open('w') as f:subprocess.run(['/tmp/wago-integer-select-local-retained-profile','profile','record','--module',str(base/w['artifact']),'--export',w['export'],'--args',','.join(map(str,w['args'])),'--want',','.join(map(str,w['oracle']['expected'])),'--bounds','signals','--mode','prepared','--duration','3s','--backend','samply','--samply','/opt/homebrew/bin/samply','--rate','1000','--include-code','--source-maps','--out',str(out/'retained-profile')],cwd=cwd,env=e,stdout=f,stderr=subprocess.STDOUT,check=True)
images=[x['image'] for x in json.loads((out/'retained-profile/images.json').read_text()) if x.get('image')];assert len(images)==1
code=base64.b64decode(images[0]['code']);assert code==Path('/tmp/integer-select-narrow-app/ml-kmeans.bin').read_bytes()
(out/'retained-profile-proof.txt').write_text(f'Final defaultON noexperimentalhook: exact1342930999 passes; {len(code)}B timed/qualified native image unchanged.\n')
with (out/'retained-profile-assembly.txt').open('w') as f:subprocess.run(['/tmp/wago-profile-tools/bin/python','/Users/work/Code/Wago/wago/experiments/arm64-parity/sampled_assembly.py',str(out/'retained-profile')],stdout=f,check=True)
