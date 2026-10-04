from pathlib import Path
import subprocess,os,json,hashlib
p=Path(__file__).parent;e=p.parent;r=e.parent/'pr802-final'
env=dict(os.environ,GOCACHE=str(e/'go-cache'),GOFLAGS='-buildvcs=false',XDG_CACHE_HOME=str(e/'cache'),PATH='/home/jtenner/Projects/wago/.tools/wabt-1.0.41-linux-x64/bin:'+os.environ['PATH'],WAGO_SPEC_INTERPRETER='/home/jtenner/Projects/wago/.tools/spec-interpreter-9d36019973201a19f9c9ebb0f10828b2fe2374aa/wasm',WAGO_SPEC_INTERPRETER_REVISION='9d36019973201a19f9c9ebb0f10828b2fe2374aa')
runs=[]
def run(name,cmd,cwd=r,extra={}):
 print(name,'start',flush=True)
 with(p/(name+'.txt')).open('w') as f: res=subprocess.run(cmd,cwd=cwd,env=dict(env,**extra),stdout=f,stderr=subprocess.STDOUT)
 runs.append(dict(name=name,command=cmd,cwd=str(cwd),exit=res.returncode));(p/'checks.json').write_text(json.dumps(runs,indent=2));print(name,res.returncode,flush=True)
 if res.returncode: raise SystemExit(1)
for name,tags in [('suite',[]),('diagnostic',['-tags=wago_codegenstats'])]:run(name+'-build',['go','test','-c',*tags,'-o',str(p/('F-'+name+'.test')),'./bench/suite'])
run('runtime-build',['go','build','-trimpath','-ldflags=-s -w','-o',str(p/'F-runtime-memory'),str(e/'runtime_memory.go')])
qemu='/home/jtenner/.codex/visualizations/2026/10/03/01a103ee-af5e-7f11-a74d-8c68fd8e53a4/wago-pr802-recovery/qemu/usr/bin/qemu-aarch64'
for checked in [False,True]:
 tags=['-tags=wago_regalloccheck'] if checked else []
 for pkg,pattern in [('src/wago','^TestSharedScalar'),('src/core/compiler/backend/railshot/shared','TestScalar')]:
  name='arm64-'+pkg.split('/')[-1]+str(checked);b=str(p/(name+'.test'))
  run(name+'-build',['go','test','-c',*tags,'-o',b,'./'+pkg],extra=dict(GOOS='linux',GOARCH='arm64'))
  run(name,[qemu,b,'-test.run='+pattern,'-test.v'])
run('arm64-diagnostic-build',['go','test','-c','-tags=wago_codegenstats','-o',str(p/'F-arm64-diagnostic.test'),'./bench/suite'],extra=dict(GOOS='linux',GOARCH='arm64'))
run('arm64-diagnostic',[qemu,str(p/'F-arm64-diagnostic.test'),'-test.run=^TestSharingDiagnostic$','-test.v','-wago.corpus=tiny,fib_rec,many_funcs,xxhash,json-as'])
for name,cwd,tags in [('root',r,[]),('root-checked',r,['-tags=wago_regalloccheck']),('bench-checked',r/'bench',['-tags=wago_regalloccheck'])]:run(name,['go','test','-p','1',*tags,'./...'],cwd)
manifest={'F':{'source':subprocess.check_output(['git','rev-parse','HEAD'],cwd=r,text=True).strip(),'harness':{str(f.relative_to(r)):hashlib.sha256(f.read_bytes()).hexdigest() for f in (r/'bench/suite').glob('sharing_*_test.go')},'binaries':{f.name:{'bytes':f.stat().st_size,'sha256':hashlib.sha256(f.read_bytes()).hexdigest()}for f in p.glob('F-*') if f.is_file()}}}
(p/'build-manifest.json').write_text(json.dumps(manifest,indent=2))
