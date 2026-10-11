import os,subprocess,json,statistics
from pathlib import Path
out=Path('/tmp/integer-select-narrow'); e=os.environ.copy();e.update(GOWORK='off',WAGO_SHARED_SCALAR='0')
rows=[]
for phase in ('exec','compile'):
 for pair in range(4):
  for candidate in ([False,True] if pair%2==0 else [True,False]):
   ee=e|dict(WAGO_ARM64_EXPERIMENT_INTEGER_SELECT_READ=str(int(candidate)))
   exe='/tmp/paired-integer-select-narrow' if candidate else '/tmp/paired-next-20261011-base'
   label='candidate' if candidate else 'baseline'
   for kind,names in [('app','ml-kmeans,geo-polygon-clipping,document-layout,audio-adpcm,video-dct'),('core','render')]:
    cmd=[exe,'-option','loop-memory-base','-workloads',names,'-phase',phase,'-rounds','1','-budget','100ms']
    if kind=='core':cmd+=['-corpus','/Users/work/Code/Web/wasm.fyi/.wasmbench/corpus-cache','-core-exec' if phase=='exec' else '-core-compile']
    if phase=='exec':cmd+=['-code-dir',str(out/('cross-images-'+label+'-'+kind))]
    p=out/('cross-'+phase+'-'+kind+'-'+str(pair)+'-'+label+'.raw')
    with p.open('w') as f:subprocess.run(cmd,env=ee,stdout=f,stderr=subprocess.STDOUT,check=True)
    for line in p.read_text().splitlines():
     if line.startswith('{'):
      r=json.loads(line);r.update(source=label,pair=pair);rows.append(r)
    (out/'cross.jsonl').write_text(''.join(json.dumps(x)+'\n' for x in rows))
 lines=['|workload|phase|paired change|base us|candidate us|','|---|---|---:|---:|---:|']
 for name,ph in sorted({(r['workload'],r['phase']) for r in rows}):
  d={(r['pair'],r['source']):r['us'] for r in rows if r['workload']==name and r['phase']==ph and r['on']}
  ratios=[100*(d[(i,'candidate')]/d[(i,'baseline')]-1) for i in range(4)]
  lines.append(f'|{name}|{ph}|{statistics.median(ratios):+.2f}%|{statistics.median(d[(i,"baseline")] for i in range(4)):.3f}|{statistics.median(d[(i,"candidate")] for i in range(4)):.3f}|')
 (out/'cross-summary.md').write_text('\n'.join(lines)+'\n');print(out/'cross-summary.md',flush=True)
