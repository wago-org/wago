from pathlib import Path
import json, os, subprocess, time

root=Path('/home/dev/Projects/dewdrop')
tree=root/'.tmp/wago-gc-read-regions-public'
out=root/'.tmp/wago-next-gc-perf/read-regions/full-tests-public'
out.mkdir(exist_ok=False)
env=dict(os.environ,CGO_ENABLED='0',GOMAXPROCS='2',DOTNET_SYSTEM_GLOBALIZATION_INVARIANT='1',
 WAGO_SPEC_INTERPRETER='/home/dev/Projects/wago/.tools/spec-interpreter-9d36019973201a19f9c9ebb0f10828b2fe2374aa/wasm',
 WAGO_SPEC_INTERPRETER_REVISION='9d36019973201a19f9c9ebb0f10828b2fe2374aa')
env.pop('GOWORK',None)
rows=[]
jobs=[('spec-corpora',['git','submodule','update','--init','--','tests/conformance/spec-v1','tests/conformance/spec-v2','tests/conformance/spec-v3'],tree,env),
 ('combined-normal',['go','test','-p','2','-count=1','./...'],tree,env),
 ('combined-checked',['go','test','-p','2','-tags=wago_regalloccheck','-count=1','./...'],tree,env),
 ('bench-checked',['go','test','-p','2','-tags=wago_regalloccheck','-count=1','./...'],tree/'bench',dict(env,GOWORK='off'))]
for label,cmd,cwd,selected in jobs:
 start=time.monotonic();p=subprocess.run(cmd,cwd=cwd,env=selected,capture_output=True,text=True)
 row=dict(label=label,command=cmd,cwd=str(cwd),seconds=time.monotonic()-start,exit=p.returncode)
 (out/(label+'.log')).write_text(p.stdout+p.stderr)
 rows.append(row);(out/'samples.json').write_text(json.dumps(rows,indent=2)+'\n')
 print(json.dumps(row),flush=True)
 if p.returncode:
  print((p.stdout+p.stderr)[-14000:],flush=True)
  raise SystemExit(p.returncode)
