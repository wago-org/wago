#!/usr/bin/env python3
import subprocess,pathlib,tempfile,tarfile,json,os
out=pathlib.Path.cwd()/'docs/rfwgc-evidence/stage7-evacuation'
old=out/'checked-fixture-correction';old.mkdir(exist_ok=True)
for p in list(out.glob('edges-*.jsonl'))+list(out.glob('edges-*.json')):p.rename(old/p.name)
pin=subprocess.check_output(['git','rev-parse','HEAD'],text=True).strip();work=pathlib.Path(tempfile.mkdtemp(prefix='wago-stage7-edges-'));archive=work/'source.tar'
with archive.open('wb') as f:subprocess.run(['git','archive',pin],stdout=f,check=True)
with tarfile.open(archive) as f:f.extractall(work,filter='data')
archive.unlink();(out/'edges-source.json').write_text(json.dumps({'pin':pin,'export':str(work)},indent=2)+'\n')
results=[];go='/home/dev/.local/share/mise/installs/go/1.27.3/bin/go'
for mode in ['ordinary','wagodebug','wago_regalloccheck']:
 for race in [False,True]:
  cmd=[go,'test','-json','-count=1','-timeout=3m','./src/core/runtime/gc/native','-run','^TestEvacuationStage7(ExactAdmissionEdges|CheckedGeneration|FullHeapZeroDemand)$']+([] if mode=='ordinary' else ['-tags='+mode])+(['-race'] if race else [])
  name=mode+('-race' if race else '')
  with (out/('edges-'+name+'.jsonl')).open('w') as f:r=subprocess.run(cmd,cwd=work,env=dict(os.environ,GOMAXPROCS='4'),stdout=f,stderr=subprocess.STDOUT)
  results.append({'mode':mode,'race':race,'command':cmd,'exit':r.returncode});(out/'edges-results.json').write_text(json.dumps(results,indent=2)+'\n');print(name,r.returncode,flush=True)
raise SystemExit(any(x['exit'] for x in results))
