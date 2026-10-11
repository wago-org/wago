import os,subprocess,json,shutil
from pathlib import Path
out=Path('/tmp/cset-lineage-retained');out.mkdir(exist_ok=True)
cwd='/tmp/wago-cset-lineage-retained-7257c1c0'
e=os.environ.copy();e.update(GOWORK='off',WAGO_SHARED_SCALAR='0',WAGO_PARITY_CACHE='/Users/work/Code/Web/wasm.fyi/.wasmbench/corpus-cache',WAGO_PARITY_CORPUS='/Users/work/Code/Web/wasm.fyi/corpora/applications')
e.pop('WAGO_ARM64_EXPERIMENT_CSET_LINEAGE',None);e.pop('WAGO_ARM64_NO_CSET_LINEAGE',None)
with (out/'strict-diagnostics.txt').open('w') as f:subprocess.run(['go','test','-tags','wago_guardpage,wago_codegenstats,wago_regalloccheck','./src/core/compiler/backend/railshot/arm64','./src/core/compiler/optimization','-run','TestCsetLineage|TestHiddenOptimizationFamilies|TestOptimizationPolicy','-count=1'],cwd=cwd,env=e,stdout=f,stderr=subprocess.STDOUT,check=True)
print('Retained strict diagnostics/policy PASS',flush=True)
for mode,tags in [('signal','wago_guardpage'),('explicit','')]:
 exe='/tmp/parity-cset-lineage-retained-'+mode+'.test'
 subprocess.run(['go','test','-c']+(['-tags',tags] if tags else [])+['-o',exe,'./experiments/arm64-parity'],cwd=cwd,env=e,check=True)
 for on in (True,False):
  ee=e|dict(WAGO_ARM64_NO_CSET_LINEAGE=str(int(not on)))
  for kind,cmd,extra in [('core',['-test.run','^TestCachedCoreOracles$','-test.v'],dict(WAGO_PARITY_CORE_CODE_DIR=str(out/(mode+'-'+str(int(on))+'-core')))),('app',['-test.run','^$','-test.bench','^BenchmarkParity/.*/Exec$','-test.benchtime=1x','-test.count=1'],dict(WAGO_PARITY_CODE_DIR=str(out/(mode+'-'+str(int(on))+'-app'))))]:
   with (out/(mode+'-'+str(int(on))+'-'+kind+'.txt')).open('w') as f:subprocess.run([exe]+cmd,env=ee|extra,stdout=f,stderr=subprocess.STDOUT,check=True)
 print('Retained'+mode+' defaultON/rollbackOFF all148 exact PASS',flush=True)
changes=json.loads(Path('/tmp/cset-lineage-inventory/changes.json').read_text());proof=[]
for kind,expected in [('core',46),('app',102)]:
 affected={v['name'] for v in changes[kind]}
 base=Path('/tmp/next-20261011-dispatch-on-'+kind);first=Path('/tmp/cset-lineage-inventory/'+kind)
 actual=out/('signal-1-'+kind)
 assert len(list(actual.glob('*.bin')))==expected
 for p in actual.glob('*.bin'):
  assert (out/('signal-0-'+kind)/p.name).read_bytes()==(base/p.name).read_bytes(),p.name
  wanted=first/p.name if p.stem in affected else base/p.name
  assert p.read_bytes()==wanted.read_bytes(),p.name
 proof.append(kind+': all retained defaultON affected images exact frozen first/timed candidate; all unaffected exact currentfour baseline; all rollbackOFF exact currentfour baseline')
proof.append('Existing register-VM dispatch1224B image preserved; no new changes outside the14 qualified CSET-lineage images.')
(out/'native-proof.txt').write_text('\n'.join(proof)+'\n');print('Retained all148 defaultON/rollback native image identity PASS',flush=True)
shutil.copytree(out,Path('/tmp/cset-lineage-ready/retained'),dirs_exist_ok=True)
