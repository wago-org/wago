from pathlib import Path
import json,os,statistics,subprocess
work=Path('/tmp/wago-vector-next-37174169')
out=work/'experiments/arm64-parity/literal-memory-index'
env=dict(os.environ,WAGO_SHARED_SCALAR='0',WAGO_BOUNDS='signals')
def run(args,tag,extra=None):
 p=subprocess.run(args,cwd=work,env=env|dict(extra or {}),text=True,capture_output=True)
 (out/(tag+'.raw')).write_text(p.stdout+p.stderr)
 print(tag,p.returncode,flush=True)
 if p.returncode:raise RuntimeError(p.stdout+p.stderr)
 return [json.loads(x) for x in p.stdout.splitlines() if x.startswith('{')]
run(['go','test','-tags','wago_guardpage wago_codegenstats wago_regalloccheck','./src/core/compiler/backend/railshot/arm64','./src/core/encoder/arm64','-run','^TestLiteralMemoryIndex|^TestImmutableZeroMemoryAddress|^TestZeroIndexLoad|^TestHiddenOptimization','-count=1','-v'],'bookkeeping-fixtures')
run(['go','build','-tags','wago_guardpage','-o','/tmp/paired-literal-memory-index-final','./experiments/arm64-parity/paired'],'bookkeeping-build')
rows=[]
apps='search-aho-corasick,search-horspool,language-register-vm,document-optimal-wrap,geo-polygon-clipping,ml-kmeans,video-dct,files-merkle-tree,audio-fir,document-edit-distance'
cores='compile-match,parse-edit-write,pcm-decode-seek,normalize-casefold,parse-structure,complex-roundtrip'
for phase in ['exec','compile']:
 for kind,names in [('app',apps),('core',cores)]:
  args=['/tmp/paired-literal-memory-index-final','-option','literal-memory-index','-workloads',names,'-phase',phase,'-rounds','6','-budget','180ms' if phase=='exec' else '120ms']
  if kind=='core':args+=['-corpus','/Users/work/Code/Web/wasm.fyi/.wasmbench/corpus-cache','-core-exec' if phase=='exec' else '-core-compile']
  rows+=run(args,f'scope-{kind}-{phase}')
  (out/'scope.jsonl').write_text(''.join(json.dumps(x)+'\n' for x in rows))
lines=['Six same-process reversed pairs; exact warm execution contracts verify both states. A focused screen of10changed apps and6largest changed core images, not all29cores.','', '|workload|phase|median paired change|pairs|','|---|---|---:|---:|']
for name,phase in sorted({(x['workload'],x['phase']) for x in rows}):
 data={(x['round'],x['on']):x['us'] for x in rows if x['workload']==name and x['phase']==phase}
 ratios=[100*(data[(r,True)]/data[(r,False)]-1) for r,on in data if not on]
 lines.append(f'|{name}|{phase}|{statistics.median(ratios):+.2f}%|{len(ratios)}|')
(out/'scope-summary.md').write_text('\n'.join(lines)+'\n')
print('\n'.join(lines),flush=True)
