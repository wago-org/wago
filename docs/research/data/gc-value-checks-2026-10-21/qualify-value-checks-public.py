from pathlib import Path
import subprocess,time,json,os
r=Path('/home/dev/Projects/dewdrop');tree=r/'.tmp/wago-gc-read-regions-public';out=r/'.tmp/wago-next-gc-perf/value-checks/final-public-packages';out.mkdir(parents=True,exist_ok=True)
env=dict(os.environ,CGO_ENABLED='0',GOMAXPROCS='2',DOTNET_SYSTEM_GLOBALIZATION_INVARIANT='1',WAGO_SPEC_INTERPRETER='/home/dev/Projects/wago/.tools/spec-interpreter-9d36019973201a19f9c9ebb0f10828b2fe2374aa/wasm',WAGO_SPEC_INTERPRETER_REVISION='9d36019973201a19f9c9ebb0f10828b2fe2374aa');env.pop('GOWORK',None);rows=[]
jobs=[('public-final-normal',['go','test','-p','2','-count=1','./src/core/compiler/backend/railshot/amd64','./src/wago']),('public-checked-focus',['go','test','-p','2','-tags=wago_codegenstats,wago_regalloccheck,wago_profile','-run','TestGC(ValueCheck|TestField|ReadRegion)|TestLocalSlotOrder','-count=1','-v','./src/core/compiler/backend/railshot/amd64','./src/wago'])]
for label,cmd in jobs:
 start=time.monotonic();p=subprocess.run(cmd,cwd=tree,env=env,capture_output=True,text=True);row=dict(label=label,command=cmd,cwd=str(tree),seconds=time.monotonic()-start,exit=p.returncode);rows.append(row);(out/(label+'.log')).write_text(p.stdout+p.stderr);(out/'samples.json').write_text(json.dumps(rows,indent=2));print(json.dumps(row),flush=True)
 if p.returncode:print((p.stdout+p.stderr)[-12000:],flush=True);raise SystemExit(p.returncode)
