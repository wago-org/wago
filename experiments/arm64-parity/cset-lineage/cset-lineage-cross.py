import os,subprocess,json,statistics
from pathlib import Path
out=Path('/tmp/cset-lineage-cross');out.mkdir(exist_ok=True)
e=os.environ.copy();e.update(WAGO_SHARED_SCALAR='0')
rows=[]
for phase in ('exec','compile'):
 for pair in range(4):
  for candidate in ([False,True] if pair%2==0 else [True,False]):
   ee=e|dict(WAGO_ARM64_EXPERIMENT_CSET_LINEAGE=str(int(candidate)))
   exe='/tmp/paired-cset-lineage-mitigated' if candidate else '/tmp/paired-cset-lineage-base'
   label='candidate' if candidate else 'baseline'
   for kind,names in [('app','ml-kmeans,geo-polyline-simplify,particle-physics,stats-quickselect,hardware-prime-implicants,video-motion,audio-adpcm,files-glob-match'),('core','modular-arithmetic,parse-structure')]:
    cmd=[exe,'-option','loop-memory-base','-workloads',names,'-phase',phase,'-rounds','1','-budget','100ms']
    if kind=='core':cmd+=['-corpus','/Users/work/Code/Web/wasm.fyi/.wasmbench/corpus-cache','-core-exec' if phase=='exec' else '-core-compile']
    if phase=='exec':cmd+=['-code-dir',str(out/('images-'+label+'-'+kind))]
    p=out/(phase+'-'+kind+'-'+str(pair)+'-'+label+'.raw')
    with p.open('w') as f:subprocess.run(cmd,env=ee,stdout=f,stderr=subprocess.STDOUT,check=True)
    for line in p.read_text().splitlines():
     if line.startswith('{'):
      r=json.loads(line);r.update(source=label,pair=pair);rows.append(r)
    (out/'all.jsonl').write_text(''.join(json.dumps(x)+'\n' for x in rows))
 lines=['|workload|phase|paired change|base us|candidate us|','|---|---|---:|---:|---:|']
 for name,ph in sorted({(r['workload'],r['phase']) for r in rows}):
  d={(r['pair'],r['source']):r['us'] for r in rows if r['workload']==name and r['phase']==ph and r['on']}
  ratio=statistics.median(100*(d[(i,'candidate')]/d[(i,'baseline')]-1) for i in range(4))
  lines.append(f'|{name}|{ph}|{ratio:+.2f}%|{statistics.median(d[(i,"baseline")] for i in range(4)):.3f}|{statistics.median(d[(i,"candidate")] for i in range(4)):.3f}|')
 (out/'summary.md').write_text('\n'.join(lines)+'\n');print('\n'.join(lines),flush=True)
for kind in ('app','core'):
 for p in (out/('images-baseline-'+kind)).glob('*-1.bin'):
  q=out/('images-candidate-'+kind)/p.name
  name=p.stem[:-2]
  if kind=='core':
   name={'modular-arithmetic':'wago__libtommath__modular-arithmetic','parse-structure':'wago__nanosvg__parse-structure'}[name]
  assert p.read_bytes()==Path(f'/tmp/next-20261011-select-on-{kind}/{name}.bin').read_bytes()
  assert q.read_bytes()==Path(f'/tmp/cset-lineage-inventory/{kind}/{name}.bin').read_bytes()
print('Cross native baseline exact root3; candidate exact first148 inventory PASS',flush=True)
