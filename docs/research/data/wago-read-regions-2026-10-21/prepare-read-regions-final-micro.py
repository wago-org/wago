from pathlib import Path
import subprocess,time,json,os,hashlib
r=Path('/home/dev/Projects/dewdrop');tree=r/'.tmp/wago-gc-read-regions';out=r/'.tmp/wago-next-gc-perf/read-regions/micro-final';out.mkdir(exist_ok=True);backend=tree/'src/core/compiler/backend/railshot/amd64';repl={}
for name in ['call.go','compile.go','driver.go','gc_struct.go','inline.go','gc_cast_array_get.go','gc_read_region.go']:
 copy=out/(name+'.base.txt');target=backend/name
 copy.write_bytes(b'//go:build amd64\n\npackage amd64\n' if name=='gc_read_region.go' else subprocess.check_output(['git','show','073b2f618:'+str(target.relative_to(tree))],cwd=tree));repl[str(target)]=str(copy)
(out/'control-overlay.json').write_text(json.dumps({'Replace':repl},indent=2));rows=[]
for variant in ['control','candidate']:
 cmd=['go','test','-p','2','-c','-o',str(out/(variant+'.test'))]
 if variant=='control':cmd+=['-overlay',str(out/'control-overlay.json')]
 cmd+=['./src/wago'];t=time.monotonic();run=subprocess.run(cmd,cwd=tree,env=dict(os.environ,CGO_ENABLED='0',GOMAXPROCS='2',GOWORK='off'),capture_output=True,text=True);row=dict(variant=variant,command=cmd,seconds=time.monotonic()-t,exit=run.returncode,stdout=run.stdout,stderr=run.stderr);rows.append(row)
 if not run.returncode:row['binary_sha256']=hashlib.sha256((out/(variant+'.test')).read_bytes()).hexdigest()
 (out/'build.json').write_text(json.dumps(rows,indent=2));print(json.dumps(row),flush=True)
 if run.returncode:raise SystemExit(run.returncode)
