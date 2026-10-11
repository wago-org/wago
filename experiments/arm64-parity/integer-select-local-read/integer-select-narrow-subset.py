import json,os,re,statistics,subprocess
from pathlib import Path
out=Path('/tmp/integer-select-narrow-subset');out.mkdir(exist_ok=True)
cache='/Users/work/Code/Web/wasm.fyi/.wasmbench/corpus-cache'
e=os.environ.copy();e.update(WAGO_SHARED_SCALAR='0',WAGO_BOUNDS='signals',WAGO_ARM64_EXPERIMENT_INTEGER_SELECT_READ='1')
app=[];core=[]
for kind in ('app','core'):
 for p in sorted(Path('/tmp/integer-select-narrow-'+kind).glob('*.bin')):
  if p.read_bytes() != (Path('/tmp/hour-20261011-'+kind)/p.name).read_bytes():
   (app if kind=='app' else core).append(p.stem)
mf=json.loads((Path(cache)/'manifest.json').read_text())['workloads']
changed=[w for w in mf if w['id'].replace('/','__') in core]
basenames=[w['id'].split('/')[-1] for w in mf]
simple=[];contracts=[]
for w in changed:
 eligible=w['oracle']['kind'] in ('exact_u64','exact_vectors') and not w['oracle'].get('memory') and not w.get('host_profile') and not w.get('initialize') and not w.get('input') and w.get('reset') in ('stateless','fresh_instance_per_sample') and basenames.count(w['id'].split('/')[-1])==1
 (simple if eligible else contracts).append(w['id'].split('/')[-1] if eligible else w['id'])
rows=[]
for phase in ('exec','compile'):
 for kind,names in [('app',app),('core',simple)]:
  cmd=['/tmp/paired-integer-select-narrow','-option','integer-select-read','-workloads',','.join(names),'-phase',phase,'-rounds','4','-budget','100ms']
  if kind=='core':cmd+=['-corpus',cache,'-core-exec' if phase=='exec' else '-core-compile']
  p=out/(kind+'-'+phase+'.raw')
  with p.open('w') as f:subprocess.run(cmd,env=e,stdout=f,stderr=subprocess.STDOUT,check=True)
  rows += [json.loads(l) for l in p.read_text().splitlines() if l.startswith('{')]
  (out/'simple.jsonl').write_text(''.join(json.dumps(x)+'\n' for x in rows))
 for name in contracts:
  for pair in range(4):
   for on in [bool(pair%2),not bool(pair%2)]:
    ee=e|dict(WAGO_PARITY_CACHE=cache,WAGO_PARITY_CORE_ID=name,WAGO_ARM64_EXPERIMENT_INTEGER_SELECT_READ=str(int(on)))
    P=phase.capitalize()
    p=out/('contract-'+name.replace('/','__')+'-'+phase+'-'+str(pair)+'-'+str(int(on))+'.raw')
    with p.open('w') as f:subprocess.run(['/tmp/parity-integer-select-narrow-signal.test','-test.run=^$','-test.bench=BenchmarkCachedContract/'+P+'$','-test.benchtime=100ms','-test.count=1'],env=ee,stdout=f,stderr=subprocess.STDOUT,check=True)
    m=re.search(r'BenchmarkCachedContract/'+P+r'-\d+\s+\d+\s+([0-9.]+) ns/op',p.read_text());assert m,p.read_text()
    rows.append(dict(workload=name,phase=phase,round=pair,on=on,us=float(m[1])/1000))
    (out/'all.jsonl').write_text(''.join(json.dumps(x)+'\n' for x in rows))
 lines=['|workload|phase|paired change|OFF us|ON us|','|---|---|---:|---:|---:|']
 for name,ph in sorted({(r['workload'],r['phase']) for r in rows}):
  d={(r['round'],r['on']):r['us'] for r in rows if r['workload']==name and r['phase']==ph}
  ratios=[100*(d[(i,True)]/d[(i,False)]-1) for i in range(4)]
  lines.append(f'|{name}|{ph}|{statistics.median(ratios):+.2f}%|{statistics.median(d[(i,False)] for i in range(4)):.3f}|{statistics.median(d[(i,True)] for i in range(4)):.3f}|')
 (out/'summary.md').write_text('\n'.join(lines)+'\n')
 print(out/'summary.md',flush=True)
