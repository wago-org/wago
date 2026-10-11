import os,subprocess,json,statistics
from pathlib import Path
out=Path('/tmp/cset-lineage-subset');out.mkdir(exist_ok=True)
e=os.environ.copy();e.update(WAGO_SHARED_SCALAR='0',WAGO_ARM64_EXPERIMENT_CSET_LINEAGE='1')
changes=json.loads(Path('/tmp/cset-lineage-inventory/changes.json').read_text())
cache='/Users/work/Code/Web/wasm.fyi/.wasmbench/corpus-cache'
mf=json.loads((Path(cache)/'manifest.json').read_text())['workloads']
core=[w['id'].split('/')[-1] for w in mf if w['id'].replace('/','__') in {v['name'] for v in changes['core']}]
app=[v['name'] for v in changes['app']]
rows=[]
for phase in ('exec','compile'):
 for kind,names in [('app',app),('core',core)]:
  cmd=['/tmp/paired-cset-lineage-mitigated','-option','cset-lineage','-workloads',','.join(names),'-phase',phase,'-rounds','4','-budget','100ms']
  if kind=='core':cmd+=['-corpus',cache,'-core-exec' if phase=='exec' else '-core-compile']
  if phase=='exec':cmd+=['-code-dir',str(out/('images-'+kind))]
  p=out/(kind+'-'+phase+'.raw')
  with p.open('w') as f:subprocess.run(cmd,env=e,stdout=f,stderr=subprocess.STDOUT,check=True)
  rows += [json.loads(l) for l in p.read_text().splitlines() if l.startswith('{')]
  (out/'all.jsonl').write_text(''.join(json.dumps(x)+'\n' for x in rows))
 lines=['|workload|phase|paired change|OFF us|ON us|','|---|---|---:|---:|---:|']
 for name,ph in sorted({(r['workload'],r['phase']) for r in rows}):
  d={(r['round'],r['on']):r['us'] for r in rows if r['workload']==name and r['phase']==ph}
  ratio=statistics.median(100*(d[(i,True)]/d[(i,False)]-1) for i in range(4))
  lines.append(f'|{name}|{ph}|{ratio:+.2f}%|{statistics.median(d[(i,False)] for i in range(4)):.3f}|{statistics.median(d[(i,True)] for i in range(4)):.3f}|')
 (out/'summary.md').write_text('\n'.join(lines)+'\n');print('\n'.join(lines),flush=True)
for kind,items in changes.items():
 for v in items:
  name=v['name']
  if kind=='core':name=next(w['id'].split('/')[-1] for w in mf if w['id'].replace('/','__')==name)
  assert (out/f'images-{kind}/{name}-1.bin').read_bytes()==Path(f'/tmp/cset-lineage-inventory/{kind}/{v["name"]}.bin').read_bytes()
  assert (out/f'images-{kind}/{name}-0.bin').read_bytes()==Path(f'/tmp/next-20261011-select-on-{kind}/{v["name"]}.bin').read_bytes()
print('All14 affected native ON/OFF exact first candidate/source baseline PASS',flush=True)
