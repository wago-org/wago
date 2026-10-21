from pathlib import Path
import json,os,subprocess,time
r=Path('/home/dev/Projects/dewdrop');tree=r/'.tmp/wago-gc-value-checks';out=r/'.tmp/wago-next-gc-perf/value-checks/final-package-tests';out.mkdir(exist_ok=True);rows=[]
env=dict(os.environ,CGO_ENABLED='0',GOMAXPROCS='2',GOWORK='off',DOTNET_SYSTEM_GLOBALIZATION_INVARIANT='1',WAGO_SPEC_INTERPRETER='/home/dev/Projects/wago/.tools/spec-interpreter-9d36019973201a19f9c9ebb0f10828b2fe2374aa/wasm',WAGO_SPEC_INTERPRETER_REVISION='9d36019973201a19f9c9ebb0f10828b2fe2374aa')
packages=['./src/core/compiler/backend/railshot/amd64','./src/wago']
for label,flags in [('final-normal',[]),('final-checked',['-tags=wago_regalloccheck']),('final-profile',['-tags=wago_codegenstats,wago_regalloccheck,wago_profile','-run','TestGC(ValueCheck|TestField|ReadRegion|TrappingGuard)|TestLocalSlotOrder'])]:
 cmd=['go','test','-p','2',*flags,'-count=1',*packages];t=time.monotonic();p=subprocess.run(cmd,cwd=tree,env=env,capture_output=True,text=True);row=dict(label=label,command=cmd,cwd=str(tree),seconds=time.monotonic()-t,exit=p.returncode);rows.append(row);(out/(label+'.log')).write_text(p.stdout+p.stderr);(out/'samples.json').write_text(json.dumps(rows,indent=2));print(json.dumps(row),flush=True)
 if p.returncode:print((p.stdout+p.stderr)[-12000:],flush=True);raise SystemExit(p.returncode)
for script in ['qualify-value-checks-public.py','finish-value-checks-admission.py']:
 subprocess.run(['python3',str(r/'.tmp/wago-next-gc-perf'/script)],cwd=r,check=True)
