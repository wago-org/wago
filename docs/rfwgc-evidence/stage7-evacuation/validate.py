#!/usr/bin/env python3
"""Correctness qualification of one immutable git export; never benchmarks."""
import hashlib,json,os,pathlib,shutil,subprocess,tarfile,tempfile
root=pathlib.Path.cwd();out=root/'docs/rfwgc-evidence/stage7-evacuation'
pin=subprocess.check_output(['git','rev-parse','HEAD'],text=True).strip()
work=pathlib.Path(tempfile.mkdtemp(prefix='wago-stage7-export-'))
archive=work/'source.tar'
with archive.open('wb') as f:subprocess.run(['git','archive',pin],stdout=f,check=True)
with tarfile.open(archive) as f:f.extractall(work,filter='data')
archive.unlink()
go='/home/dev/.local/share/mise/installs/go/1.27.3/bin/go'
env=dict(os.environ,GOMAXPROCS='4')
hashes={str(p.relative_to(work)):hashlib.sha256(p.read_bytes()).hexdigest() for p in sorted(work.rglob('*.go')) if '.tools' not in p.parts}
(out/'source.json').write_text(json.dumps({'pin':pin,'export':str(work),'go':subprocess.check_output([go,'version'],text=True).strip(),'sha256':hashes},indent=2)+'\n')
results=[]
for mode in ['ordinary','wagodebug','wago_regalloccheck']:
 flags=[] if mode=='ordinary' else ['-tags='+mode]
 for suite,args in [('collector',['./src/core/runtime/gc/...']),('race',['-race','./src/core/runtime/gc/...']),('guest',['./src/wago','-run','^TestGC']),('backend',['./src/core/compiler/backend/railshot/amd64','-run','GC|Collector|NativeAlloc|Eden']),('cli',['./cli/runtime/commands/run','./cli/internal/handoff','./cli/manager/internal/plugin','-run','GC|RuntimeCommands|Scope'])]:
  cmd=[go,'test','-json','-p=3',*flags,*args,'-count=1','-timeout=10m']
  log=out/f'{mode}-{suite}.jsonl'
  with log.open('w') as f:r=subprocess.run(cmd,cwd=work,env=env,stdout=f,stderr=subprocess.STDOUT)
  counts={k:[] for k in ('pass','fail','skip')}
  for line in log.read_text().splitlines():
   try:item=json.loads(line)
   except ValueError:continue
   if item.get('Action') in counts and 'Test' in item:counts[item['Action']].append(item['Package']+':'+item['Test'])
  results.append({'mode':mode,'suite':suite,'command':cmd,'exit':r.returncode,'counts':{k:len(v) for k,v in counts.items()},'fail':counts['fail'],'skip':counts['skip']})
  (out/'results.json').write_text(json.dumps(results,indent=2)+'\n')
  print(mode,suite,r.returncode,results[-1]['counts'],flush=True)
print('export',work,flush=True)
raise SystemExit(any(x['exit'] for x in results))
