from pathlib import Path
import subprocess,time,json,os
r=Path('/home/dev/Projects/dewdrop');tree=r/'.tmp/wago-gc-value-checks';out=r/'.tmp/wago-next-gc-perf/value-checks/full-tests-combined';out.mkdir(parents=True,exist_ok=True);env=dict(os.environ,CGO_ENABLED='0',GOMAXPROCS='2',DOTNET_SYSTEM_GLOBALIZATION_INVARIANT='1',WAGO_SPEC_INTERPRETER='/home/dev/Projects/wago/.tools/spec-interpreter-9d36019973201a19f9c9ebb0f10828b2fe2374aa/wasm',WAGO_SPEC_INTERPRETER_REVISION='9d36019973201a19f9c9ebb0f10828b2fe2374aa');env.pop('GOWORK',None);rows=[]
jobs=[('combined-normal',['go','test','-p','2','-count=1','./...'],tree,env),('combined-checked',['go','test','-p','2','-tags=wago_regalloccheck','-count=1','./...'],tree,env),('bench-checked',['go','test','-p','2','-tags=wago_regalloccheck','-count=1','./...'],tree/'bench',dict(env,GOWORK='off')),('focus-profile-final',['go','test','-p','2','-tags=wago_codegenstats,wago_regalloccheck,wago_profile','-run','^TestGCValueCheck','-count=1','-v','./src/core/compiler/backend/railshot/amd64','./src/wago'],tree,dict(env,GOWORK='off'))]
for label,cmd,cwd,selected in jobs:
 t=time.monotonic();p=subprocess.run(cmd,cwd=cwd,env=selected,capture_output=True,text=True);row=dict(label=label,command=cmd,cwd=str(cwd),seconds=time.monotonic()-t,exit=p.returncode);rows.append(row);(out/(label+'.log')).write_text(p.stdout+p.stderr);(out/'finish-samples.json').write_text(json.dumps(rows,indent=2));print(json.dumps(row),flush=True)
 if p.returncode:
  print((p.stdout+p.stderr)[-16000:],flush=True);raise SystemExit(p.returncode)
