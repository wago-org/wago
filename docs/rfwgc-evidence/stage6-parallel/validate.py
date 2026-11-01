#!/usr/bin/env python3
"""Snapshot committed tree plus owned collector overlay; correctness only."""
import hashlib,json,os,pathlib,shutil,subprocess,tarfile,tempfile
root=pathlib.Path.cwd();out=root/'docs/rfwgc-evidence/stage6-parallel'
out=out/'final';out.mkdir(exist_ok=True)
base=subprocess.check_output(['git','rev-parse','HEAD'],text=True).strip()
work=pathlib.Path(tempfile.mkdtemp(prefix='wago-stage6-'))
archive=work/'source.tar'
with archive.open('wb') as f:subprocess.run(['git','archive',base],stdout=f,check=True)
with tarfile.open(archive) as f:f.extractall(work,filter='data')
archive.unlink()
owned=root/'src/core/runtime/gc/native'
shutil.rmtree(work/'src/core/runtime/gc/native')
shutil.copytree(owned,work/'src/core/runtime/gc/native')
hashes={str(p.relative_to(root)):hashlib.sha256(p.read_bytes()).hexdigest() for p in sorted(owned.glob('*.go'))}
(out/'source.json').write_text(json.dumps({'base':base,'overlay':hashes,'archive':str(work),'callback_fixture_sha256':hashlib.sha256((out.parent/'callback_integration_test.go.txt').read_bytes()).hexdigest()},indent=2)+'\n')
go='/home/dev/.local/share/mise/installs/go/1.27.3/bin/go'
env=dict(os.environ,GOMAXPROCS='4')
results=[]
for mode in ['ordinary','wagodebug','wago_regalloccheck']:
 flags=[] if mode=='ordinary' else ['-tags='+mode]
 for suite,args in [('collector',['./src/core/runtime/gc/...']),('race',['-race','./src/core/runtime/gc/native','-run','Parallel|R9Protocol']),('backend',['./src/core/compiler/backend/railshot/amd64','-run','GC|Collector|NativeAlloc|Eden']),('guest',['./src/wago','-run','^TestGC'])]:
  cmd=[go,'test','-p=4',*flags,*args,'-count=1','-v','-timeout=10m']
  log=out/f'{mode}-{suite}.txt'
  with log.open('w') as f:r=subprocess.run(cmd,cwd=work,env=env,stdout=f,stderr=subprocess.STDOUT)
  results.append({'mode':mode,'suite':suite,'command':cmd,'exit':r.returncode})
  (out/'results.json').write_text(json.dumps(results,indent=2)+'\n')
  print(mode,suite,r.returncode,flush=True)
raise SystemExit(any(x['exit'] for x in results))
