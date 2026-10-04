from pathlib import Path
import subprocess,os,json
p=Path(__file__).parent; roots={'M':p.parent/'pr802-main','P':p.parent/'pr802-final'}
env=dict(os.environ,GOCACHE=str(p/'go-cache'),XDG_CACHE_HOME=str(p/'cache'),GOFLAGS='-buildvcs=false',PATH='/home/jtenner/Projects/wago/.tools/wabt-1.0.41-linux-x64/bin:'+os.environ['PATH'],WAGO_SPEC_INTERPRETER='/home/jtenner/Projects/wago/.tools/spec-interpreter-9d36019973201a19f9c9ebb0f10828b2fe2374aa/wasm',WAGO_SPEC_INTERPRETER_REVISION='9d36019973201a19f9c9ebb0f10828b2fe2374aa')
runs=[]
for rev,r in roots.items():
 cmd=['git','submodule','update','--init','--depth','1','tests/conformance/spec-v1','tests/conformance/spec-v2','tests/conformance/spec-v3']
 with(p/(rev+'-submodule.txt')).open('w')as f:res=subprocess.run(cmd,cwd=r,env=env,stdout=f,stderr=subprocess.STDOUT)
 print(rev,'submodule',res.returncode,flush=True)
 if res.returncode:raise SystemExit(1)
 for name,cwd,tags in [('root',r,[]),('root-checked',r,['-tags=wago_regalloccheck']),('bench-checked',r/'bench',['-tags=wago_regalloccheck'])]:
  cmd=['go','test','-p','1',*tags,'./...'];print(rev,name,'start',flush=True)
  with(p/(rev+'-'+name+'.txt')).open('w')as f:res=subprocess.run(cmd,cwd=cwd,env=env,stdout=f,stderr=subprocess.STDOUT)
  runs.append(dict(revision=rev,name=name,command=cmd,cwd=str(cwd),exit=res.returncode));(p/'final-gates.json').write_text(json.dumps(runs,indent=2))
  print(rev,name,res.returncode,flush=True)
