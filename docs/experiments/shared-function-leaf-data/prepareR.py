from pathlib import Path
import os,subprocess,json,hashlib
p=Path(__file__).resolve().parent
roots={k:p.parent.parent/v for k,v in [('A','pr802-leaf-main'),('C','pr802-leaf-parent'),('R','pr802-final')]}
env=dict(os.environ,GOFLAGS='-buildvcs=false',CGO_ENABLED='0',GOAMD64='v1',GOARM64='v8.0',GOMAXPROCS='1',GOGC='100',GOMEMLIMIT='off',TERM='dumb')
env.pop('WAGO_SHARED_SCALAR',None)
runs=[]
def run(name,cmd,root,extra={}):
 print(name,flush=True)
 with(p/(name+'.log')).open('w')as f:r=subprocess.run([str(x) for x in cmd],cwd=root,env=dict(env,**extra),stdout=f,stderr=subprocess.STDOUT)
 runs.append(dict(name=name,command=[str(x)for x in cmd],cwd=str(root),exit=r.returncode));(p/'final-prepare-runs.json').write_text(json.dumps(runs,indent=2))
 if r.returncode:raise SystemExit(name+' failed; see log')
for rev,root in roots.items():
 for kind,tags in [('suite',[]),('checked',['-tags=wago_regalloccheck']),('diagnostic',['-tags=wago_codegenstats'])]:
  run(rev+'-'+kind+'-build',['go','test','-c',*tags,'-o',p/(rev+'-'+kind+'.test'),'./bench/suite'],root)
 run(rev+'-runtime-build',['go','build','-trimpath','-ldflags=-s -w','-o',p/(rev+'-runtime-memory'),p.parent/'runtime_memory.go'],root)
 for kind in ['suite','checked']:
  run(rev+'-'+kind+'-correctness',[p/(rev+'-'+kind+'.test'),'-test.run=^Test(SharingFixtures|SharingZeroLocals|Corpus|CorpusSemanticExec)$','-test.count=1','-test.v','-wago.corpus=tiny,fib_rec,many_funcs,xxhash,json-as'],root/'bench/suite')
for pkg in ['src/core/compiler/backend/railshot/arm64','src/wago','bench/suite']:
 name='R-arm64-'+pkg.split('/')[-1]+'-final'
 run(name+'-build',['go','test','-c','-tags=wago_codegenstats,wago_regalloccheck','-o',p/(name+'.test'),'./'+pkg],roots['R'],{'GOOS':'linux','GOARCH':'arm64'})
 q='/home/jtenner/.codex/visualizations/2026/10/03/01a103ee-af5e-7f11-a74d-8c68fd8e53a4/wago-pr802-recovery/qemu/usr/bin/qemu-aarch64'
 pattern='^TestSharedScalar' if pkg.endswith('arm64') or pkg.endswith('wago') else '^Test(SharingFixtures|SharingZeroLocals|SharingDiagnostic)$'
 run(name,[q,p/(name+'.test'),'-test.run='+pattern,'-test.v','-test.timeout=10m',*(['-wago.corpus=tiny,fib_rec,many_funcs,xxhash,json-as']if pkg.endswith('suite')else[])],roots['R']/pkg)
for arch in ['amd64','arm64']:
 for pkg in ['src/wago','src/core/compiler/backend/railshot/'+arch]:
  name='R-darwin-'+arch+'-'+pkg.split('/')[-1]
  run(name+'-build',['go','test','-c','-tags=wago_regalloccheck','-o',p/(name+'.test'),'./'+pkg],roots['R'],{'GOOS':'darwin','GOARCH':arch})
for rev,root in roots.items():
 run(rev+'-native-diagnostic',[p/(rev+'-diagnostic.test'),'-test.run=^TestSharingDiagnostic$','-test.v','-test.count=1','-wago.corpus=tiny,fib_rec,many_funcs,xxhash,json-as'],root/'bench/suite')
files=['bench/'+str(f.relative_to(roots['R']/'bench')) for f in (roots['R']/'bench').rglob('*') if f.is_file() and f.suffix not in ['.test']]
files += ['corpus/'+str(f.relative_to(roots['R']/'corpus')) for f in (roots['R']/'corpus').rglob('*') if f.is_file()]
hashes={rev:{f:hashlib.sha256((root/f).read_bytes()).hexdigest()for f in files}for rev,root in roots.items()}
if any(hashes[k]!=hashes['R'] for k in hashes):raise SystemExit('harness/corpus mismatch')
manifest={'sources':{k:subprocess.check_output(['git','rev-parse','HEAD'],cwd=v,text=True).strip()for k,v in roots.items()},'harness_corpus':hashes,'runtime_harness_sha256':hashlib.sha256((p.parent/'runtime_memory.go').read_bytes()).hexdigest(),'binaries':{f.name:hashlib.sha256(f.read_bytes()).hexdigest()for f in p.iterdir()if f.is_file()and(f.suffix=='.test'or f.name.endswith('runtime-memory'))}}
(p/'final-build-manifest.json').write_text(json.dumps(manifest,indent=2))
print('PREPARED',flush=True)
