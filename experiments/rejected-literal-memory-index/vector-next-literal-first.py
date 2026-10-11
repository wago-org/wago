from pathlib import Path
import hashlib,json,os,statistics,subprocess
work=Path('/tmp/wago-vector-next-37174169')
out=work/'experiments/arm64-parity/literal-memory-index'
env=dict(os.environ,WAGO_SHARED_SCALAR='0',WAGO_BOUNDS='signals')
def run(args,tag,extra=None):
 p=subprocess.run(args,cwd=work,env=env|dict(extra or {}),text=True,capture_output=True)
 (out/(tag+'.raw')).write_text(p.stdout+p.stderr)
 print(tag,p.returncode,flush=True)
 if p.returncode:raise RuntimeError(p.stdout+p.stderr)
 return [json.loads(x) for x in p.stdout.splitlines() if x.startswith('{')]
run(['go','test','-tags','wago_guardpage wago_codegenstats wago_regalloccheck','./src/core/compiler/backend/railshot/arm64','./src/core/encoder/arm64','-run','^TestLiteralMemoryIndex|^TestImmutableZeroMemoryAddress|^TestZeroIndexLoad|^TestHiddenOptimization','-count=1','-v'],'fixtures')
run(['go','build','-tags','wago_guardpage','-o','/tmp/paired-literal-memory-index','./experiments/arm64-parity/paired'],'build')
run(['go','build','-tags','wago_guardpage','-o','/tmp/inventory-literal-memory-index','./experiments/arm64-parity/literal-memory-index/inventory.go'],'inventory-build')
for on in [0,1]:run(['/tmp/inventory-literal-memory-index',str(out/f'native-signal-{on}'),str(on)],f'inventory-{on}')
inventory=[]
for kind in ['app','core']:
 base=Path('/tmp/hour-20261011-'+kind)
 files=list((out/'native-signal-0'/kind).glob('*.bin'));changed=[];drift=[]
 for p in files:
  a=p.read_bytes();b=(out/'native-signal-1'/kind/p.name).read_bytes()
  if a!=b:changed.append(p.name)
  if (base/p.name).read_bytes()!=a:drift.append(p.name)
  inventory.append(dict(kind=kind,name=p.name,changed=a!=b,off_bytes=len(a),on_bytes=len(b),off_sha256=hashlib.sha256(a).hexdigest(),on_sha256=hashlib.sha256(b).hexdigest(),pristine_equal=(base/p.name).read_bytes()==a))
 print(kind,'count',len(files),'changed',len(changed),changed,'OFFdrift',drift,flush=True)
 assert not drift,'OFF native drift'
(out/'inventory.jsonl').write_text(''.join(json.dumps(x)+'\n' for x in inventory))
rows=[]
cache='/Users/work/Code/Web/wasm.fyi/.wasmbench/corpus-cache'
for phase,rounds,budget in [('exec',10,'250ms'),('compile',8,'150ms')]:
 args=['/tmp/paired-literal-memory-index','-option','literal-memory-index','-corpus',cache,'-core-exec' if phase=='exec' else '-core-compile','-workloads','modular-arithmetic','-phase',phase,'-rounds',str(rounds),'-budget',budget]
 rows+=run(args,'target-'+phase)
 (out/'target.jsonl').write_text(''.join(json.dumps(x)+'\n' for x in rows))
lines=['|workload|phase|median paired change|pairs|','|---|---|---:|---:|']
for name,phase in sorted({(x['workload'],x['phase']) for x in rows}):
 data={(x['round'],x['on']):x['us'] for x in rows if x['workload']==name and x['phase']==phase}
 ratios=[100*(data[(r,True)]/data[(r,False)]-1) for r,on in data if not on]
 lines.append(f'|{name}|{phase}|{statistics.median(ratios):+.2f}%|{len(ratios)}|')
(out/'target-summary.md').write_text('\n'.join(lines)+'\n')
print('\n'.join(lines),flush=True)
