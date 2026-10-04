from pathlib import Path
import subprocess,os,json,hashlib
p=Path(__file__).parent
roots={'M':p.parent/'pr802-main','R':p.parent/'pr802-rebased','Z':p.parent/'pr802-zero'}
manifest={};env=dict(os.environ,GOCACHE=str(p/'go-cache'),GOFLAGS='-buildvcs=false')
for rev,r in roots.items():
 cmd=['go','build','-trimpath','-ldflags=-s -w','-o',str(p/(rev+'-runtime-memory')),str(p/'runtime_memory.go')]
 with(p/(rev+'-runtime-memory-build.txt')).open('w')as f:res=subprocess.run(cmd,cwd=r,env=env,stdout=f,stderr=subprocess.STDOUT)
 print(rev,res.returncode,flush=True)
 if res.returncode:raise SystemExit(1)
 b=p/(rev+'-runtime-memory');manifest[rev]=dict(source=subprocess.check_output(['git','rev-parse','HEAD'],cwd=r,text=True).strip(),command=cmd,bytes=b.stat().st_size,sha256=hashlib.sha256(b.read_bytes()).hexdigest())
 (p/'runtime-stage-manifest.json').write_text(json.dumps(manifest,indent=2))
