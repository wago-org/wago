from pathlib import Path
import itertools,json,os,re,statistics,subprocess

out=Path('/tmp/wago-vector-next-37174169/experiments/arm64-parity/entry-dead-zeros')
cache='/Users/work/Code/Web/wasm.fyi/.wasmbench/corpus-cache'
binary='/tmp/paired-entry-dead-zeros-scratch'
env=dict(os.environ,WAGO_SHARED_SCALAR='0',WAGO_BOUNDS='signals')

def runpaired(exe,kind,names,phase,option,rounds,budget,tag,extraenv=None):
 e=env|dict(extraenv or {})
 args=[exe,'-option',option,'-workloads',names,'-phase',phase,'-rounds',str(rounds),'-budget',budget]
 if kind=='core':args+=['-corpus',cache,'-core-exec' if phase=='exec' else '-core-compile']
 if (out/(tag+'.raw')).exists() and 'panic:' not in (out/(tag+'.raw')).read_text():
  return [json.loads(l) for l in (out/(tag+'.raw')).read_text().splitlines() if l.startswith('{')]
 p=subprocess.run(args,env=e,text=True,capture_output=True)
 (out/(tag+'.raw')).write_text(p.stdout+p.stderr)
 if p.returncode:raise RuntimeError((tag,p.returncode,p.stdout,p.stderr))
 return [json.loads(l) for l in p.stdout.splitlines() if l.startswith('{')]

def summarize(rows,path,variants=('off','on')):
 lines=['|workload|phase|median paired change|pairs|','|---|---|---:|---:|']
 for name,phase in sorted({(x['workload'],x['phase']) for x in rows}):
  data={}
  for x in rows:
   if x['workload']==name and x['phase']==phase:data[(x['round'],x.get('variant','on' if x['on'] else 'off'))]=x['us']
  for other in variants[1:]:
   ratios=[100*(data[(r,other)]/data[(r,variants[0])]-1) for r,v in data if v==variants[0] and (r,other) in data]
   if ratios:lines.append(f'|{name} ({other} vs {variants[0]})|{phase}|{statistics.median(ratios):+.2f}%|{len(ratios)}|')
 path.write_text('\n'.join(lines)+'\n')
 print(path.read_text(),flush=True)

# Same-process paired changed-image subset; all simple cores selected by unique basename.
rows=[]
for phase,rounds,budget in [('exec',8,'200ms'),('compile',6,'120ms')]:
 for kind,names in [('app','stats-gini'),('core','modular-arithmetic,pcm-decode-seek,hashN,compile-match')]:
  rows+=runpaired(binary,kind,names,phase,'entry-dead-zeros',rounds,budget,f'scratch-subset-{kind}-{phase}')
 (out/'scratch-subset.jsonl').write_text(''.join(json.dumps(x)+'\n' for x in rows))
summarize(rows,out/'scratch-subset-summary.md')

# Full IDs preserve the nussinov identity and zstd input/initialization contracts.
rows=[]
for phase in ['Exec','Compile']:
 for name in ['wago/coremark/2k-performance','wago/zstd/decompress','wago/polybench-nussinov/polybench_run']:
  for pair in range(4):
   for on in [bool(pair%2),not bool(pair%2)]:
    e=env|dict(WAGO_PARITY_CACHE=cache,WAGO_PARITY_CORE_ID=name,WAGO_ARM64_EXPERIMENT_ENTRY_DEAD_ZEROS=str(int(on)))
    p=subprocess.run(['/tmp/parity-entry-dead-zeros-scratch.test','-test.run=^$','-test.bench=BenchmarkCachedContract/'+phase+'$','-test.benchtime=150ms','-test.count=1'],env=e,text=True,capture_output=True)
    (out/f'scratch-contract-{name.split("/")[1]}-{phase.lower()}-{pair}-{int(on)}.raw').write_text(p.stdout+p.stderr)
    if p.returncode:raise RuntimeError((name,p.returncode,p.stdout,p.stderr))
    match=re.search(r'BenchmarkCachedContract/'+phase+r'-\d+\s+\d+\s+([0-9.]+) ns/op',p.stdout)
    assert match,p.stdout
    rows.append(dict(workload=name,phase=phase.lower(),round=pair,on=on,us=float(match[1])/1000))
    (out/'scratch-contract.jsonl').write_text(''.join(json.dumps(x)+'\n' for x in rows))
summarize(rows,out/'scratch-contract-summary.md')

# Exact-source controls separate the disabled source tax from enabled analysis cost.
rows=[]
variants={'base':('/tmp/paired-next-20261011-base','0'),'off':(binary,'0'),'on':(binary,'1')}
orders=list(itertools.permutations(variants))
for phase in ['compile','exec']:
 for pair,order in enumerate(orders):
  for variant in order:
   exe,value=variants[variant]
   for kind,names in [('app','audio-adpcm,language-register-vm,files-glob-match,stats-gini'),('core','modular-arithmetic,decimal-parse,complex-roundtrip,compile-match')]:
    xs=runpaired(exe,kind,names,phase,'loop-memory-base',1,'150ms' if phase=='compile' else '200ms',f'scratch-source-{kind}-{phase}-{pair}-{variant}',dict(WAGO_ARM64_EXPERIMENT_ENTRY_DEAD_ZEROS=value))
    for x in xs:
     if x['on']:
      x.update(variant=variant,round=pair);rows.append(x)
    (out/'scratch-source.jsonl').write_text(''.join(json.dumps(x)+'\n' for x in rows))
summarize(rows,out/'scratch-source-summary.md',('base','off','on'))
