from pathlib import Path
import itertools,json,os,statistics,subprocess
work=Path('/tmp/wago-vector-next-37174169')
out=work/'experiments/arm64-parity/literal-memory-index'
env=dict(os.environ,WAGO_SHARED_SCALAR='0',WAGO_BOUNDS='signals')
def run(exe,args,tag,extra=None):
 p=subprocess.run([exe]+args,cwd=work,env=env|dict(extra or {}),text=True,capture_output=True)
 (out/(tag+'.raw')).write_text(p.stdout+p.stderr)
 if p.returncode:raise RuntimeError((tag,p.stdout,p.stderr))
 return [json.loads(x) for x in p.stdout.splitlines() if x.startswith('{')]
def summarize(rows,file,variants=None):
 lines=['|workload|phase|comparison|median paired change|IQR|pairs|','|---|---|---|---:|---:|---:|']
 for name,phase in sorted({(x['workload'],x['phase']) for x in rows}):
  data={(x['round'],x.get('variant','on' if x['on'] else 'off')):x['us'] for x in rows if x['workload']==name and x['phase']==phase}
  vs=variants or ('off','on')
  for other in vs[1:]:
   ratios=[100*(data[(r,other)]/data[(r,vs[0])]-1) for r,v in data if v==vs[0] and (r,other) in data]
   q=statistics.quantiles(ratios,n=4)
   lines.append(f'|{name}|{phase}|{other} vs {vs[0]}|{statistics.median(ratios):+.2f}%|{q[0]:+.2f}..{q[2]:+.2f}%|{len(ratios)}|')
 (out/file).write_text('\n'.join(lines)+'\n');print('\n'.join(lines),flush=True)
binary='/tmp/paired-literal-memory-index-final'
apps='search-aho-corasick,geo-polygon-clipping,files-merkle-tree,search-horspool'
rows=run(binary,['-option','literal-memory-index','-workloads',apps,'-rounds','12','-budget','250ms'],'confirm-exec')
(out/'confirm.jsonl').write_text(''.join(json.dumps(x)+'\n' for x in rows));summarize(rows,'confirm-summary.md')
variants={'base':('/tmp/paired-next-20261011-base','0'),'off':(binary,'0'),'on':(binary,'1')}
rows=[]
for pair,order in enumerate(itertools.permutations(variants)):
 for variant in order:
  exe,value=variants[variant]
  for kind,names in [('app',apps+',audio-adpcm,files-glob-match'),('core','modular-arithmetic,decimal-parse,complex-roundtrip')]:
   args=['-option','loop-memory-base','-workloads',names,'-phase','compile','-rounds','1','-budget','250ms']
   if kind=='core':args+=['-core-compile','-corpus','/Users/work/Code/Web/wasm.fyi/.wasmbench/corpus-cache']
   xs=run(exe,args,f'source-{kind}-compile-{pair}-{variant}',{'WAGO_ARM64_EXPERIMENT_LITERAL_MEMORY_INDEX':value})
   for x in xs:
    if x['on']:x.update(variant=variant,round=pair);rows.append(x)
   (out/'source.jsonl').write_text(''.join(json.dumps(x)+'\n' for x in rows))
 print('source pair',pair,'done',flush=True)
summarize(rows,'source-summary.md',('base','off','on'))
