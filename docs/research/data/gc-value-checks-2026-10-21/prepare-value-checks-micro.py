from pathlib import Path
import subprocess,json,time,os,hashlib
r=Path('/home/dev/Projects/dewdrop');tree=r/'.tmp/wago-gc-value-checks';out=r/'.tmp/wago-next-gc-perf/value-checks/micro';out.mkdir(parents=True,exist_ok=True);repl={}
files=['compile.go','driver.go','gc_direct.go','gc_resolver.go','gc_struct.go','gc_test_field.go','references.go','regalloc.go','gc_value_checks.go']
for name in files:
 path=tree/'src/core/compiler/backend/railshot/amd64'/name;copy=out/(name+'.base.txt');copy.write_bytes(b'//go:build amd64\npackage amd64\n' if name=='gc_value_checks.go' else subprocess.check_output(['git','show','40cfab8d8:'+str(path.relative_to(tree))],cwd=tree));repl[str(path)]=str(copy)
(out/'control-overlay.json').write_text(json.dumps({'Replace':repl},indent=2));rows=[]
for variant in ['control','candidate']:
 cmd=['go','test','-p','2','-c','-o',str(out/(variant+'.test'))]
 if variant=='control':cmd+=['-overlay',str(out/'control-overlay.json')]
 cmd+=['./src/wago'];t=time.monotonic();p=subprocess.run(cmd,cwd=tree,env=dict(os.environ,CGO_ENABLED='0',GOMAXPROCS='2',GOWORK='off'),capture_output=True,text=True);row=dict(variant=variant,command=cmd,seconds=time.monotonic()-t,exit=p.returncode,stdout=p.stdout,stderr=p.stderr);rows.append(row)
 if not p.returncode:row['binary_sha256']=hashlib.sha256((out/(variant+'.test')).read_bytes()).hexdigest()
 (out/'build.json').write_text(json.dumps(rows,indent=2));print(json.dumps(row),flush=True)
 if p.returncode:raise SystemExit(p.returncode)
