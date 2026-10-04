from pathlib import Path
import subprocess,os,json,hashlib
p=Path(__file__).parent
roots={'T':p.parent/'pr802-packed','P':p.parent/'pr802-final'}
env=dict(os.environ,GOCACHE=str(p/'go-cache'),GOFLAGS='-buildvcs=false');manifest={}
for rev,r in roots.items():
 manifest[rev]={'source':subprocess.check_output(['git','rev-parse','HEAD'],cwd=r,text=True).strip(),'harness':{str(f.relative_to(r)):hashlib.sha256(f.read_bytes()).hexdigest()for f in (r/'bench/suite').glob('sharing_*_test.go')}}
 for kind,tags in [('suite',[]),('diagnostic',['-tags=wago_codegenstats']),('runtime-memory',None)]:
  cmd=['go','build','-trimpath','-ldflags=-s -w','-o',str(p/(rev+'-'+kind)),str(p/'runtime_memory.go')] if tags is None else ['go','test','-c',*tags,'-o',str(p/(rev+'-'+kind+'.test')),'./bench/suite']
  with(p/(rev+'-'+kind+'-build.txt')).open('w')as f:res=subprocess.run(cmd,cwd=r,env=env,stdout=f,stderr=subprocess.STDOUT)
  print(rev,kind,res.returncode,flush=True)
  if res.returncode:raise SystemExit(1)
  b=p/(rev+'-'+kind+('' if tags is None else '.test'));manifest[rev][kind]={'command':cmd,'exit':res.returncode,'sha256':hashlib.sha256(b.read_bytes()).hexdigest(),'bytes':b.stat().st_size}
 (p/'final-build-manifest.json').write_text(json.dumps(manifest,indent=2))
