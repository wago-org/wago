from pathlib import Path
import subprocess,os,json,hashlib,shutil
p=Path(__file__).parent
roots={'M':p.parent/'pr802-main','R':p.parent/'pr802-rebased','Z':p.parent/'pr802-zero'}
env=dict(os.environ,GOCACHE=str(p/'go-cache'))
manifest={}
for rev,r in roots.items():
 manifest[rev]={'source':subprocess.check_output(['git','rev-parse','HEAD'],cwd=r,text=True).strip(),'harness':{str(f.relative_to(r)):hashlib.sha256(f.read_bytes()).hexdigest()for f in (r/'bench/suite').glob('sharing_*_test.go')}}
 for kind,tags in [('suite',[]),('diagnostic',['-tags=wago_codegenstats'])]:
  cmd=['go','test','-c',*tags,'-o',str(p/(rev+'-'+kind+'.test')),'./bench/suite']
  with(p/(rev+'-'+kind+'-build.txt')).open('w')as f:res=subprocess.run(cmd,cwd=r,env=env,stdout=f,stderr=subprocess.STDOUT)
  print(rev,kind,res.returncode,flush=True);manifest[rev][kind]={'command':cmd,'exit':res.returncode}
  if res.returncode:raise SystemExit(1)
  manifest[rev][kind]['sha256']=hashlib.sha256((p/(rev+'-'+kind+'.test')).read_bytes()).hexdigest()
 (p/'stage-build-manifest.json').write_text(json.dumps(manifest,indent=2))
