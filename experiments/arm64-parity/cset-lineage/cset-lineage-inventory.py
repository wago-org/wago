import os,subprocess,json
from pathlib import Path
out=Path('/tmp/cset-lineage-inventory');out.mkdir(exist_ok=True)
e=os.environ.copy();e.update(GOWORK='off',WAGO_SHARED_SCALAR='0',WAGO_ARM64_EXPERIMENT_CSET_LINEAGE='1',WAGO_PARITY_CACHE='/Users/work/Code/Web/wasm.fyi/.wasmbench/corpus-cache',WAGO_PARITY_CORPUS='/Users/work/Code/Web/wasm.fyi/corpora/applications')
cwd='/tmp/wago-cset-lineage-76e52be8'
subprocess.run(['go','test','-c','-tags','wago_guardpage','-o','/tmp/parity-cset-lineage-first-signal.test','./experiments/arm64-parity'],cwd=cwd,env=e,check=True)
changes={}
for kind,cmd,extra in [('core',['-test.run','^TestCachedCoreOracles$','-test.v'],dict(WAGO_PARITY_CORE_CODE_DIR=str(out/'core'))),('app',['-test.run','^$','-test.bench','^BenchmarkParity/.*/Exec$','-test.benchtime=1x','-test.count=1'],dict(WAGO_PARITY_CODE_DIR=str(out/'app')))]:
 with (out/(kind+'.txt')).open('w') as f:subprocess.run(['/tmp/parity-cset-lineage-first-signal.test']+cmd,env=e|extra,stdout=f,stderr=subprocess.STDOUT,check=True)
 base=Path('/tmp/next-20261011-select-on-'+kind)
 actual=out/kind
 assert {p.name for p in actual.glob('*.bin')}=={p.name for p in base.glob('*.bin')}
 changed=[]
 for p in sorted(actual.glob('*.bin')):
  a=(base/p.name).read_bytes();b=p.read_bytes()
  if a!=b:
   assert len(a)==len(b)
   words=sum(a[i:i+4]!=b[i:i+4] for i in range(0,len(a),4))
   changed.append(dict(name=p.stem,size=len(a),changed_words=words))
 changes[kind]=changed
(out/'changes.json').write_text(json.dumps(changes,indent=2)+'\n');print(json.dumps(changes,indent=2),flush=True)
