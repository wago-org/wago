from pathlib import Path
import subprocess,os,json
p=Path(__file__).parent;r=p.parent/'pr802-final';env=dict(os.environ,GOCACHE=str(p/'go-cache'),GOFLAGS='-buildvcs=false');runs=[]
def run(name,cmd,extra=None):
 print(name,flush=True)
 with(p/(name+'.txt')).open('w')as out:res=subprocess.run(cmd,cwd=r,env=dict(env,**(extra or {})),stdout=out,stderr=subprocess.STDOUT)
 runs.append(dict(name=name,command=cmd,exit=res.returncode));(p/'cross-checks.json').write_text(json.dumps(runs,indent=2));return res.returncode
qemu='/home/jtenner/.codex/visualizations/2026/10/03/01a103ee-af5e-7f11-a74d-8c68fd8e53a4/wago-pr802-recovery/qemu/usr/bin/qemu-aarch64'
for checked in [False,True]:
 tags=['-tags=wago_regalloccheck']if checked else[];mode='checked'if checked else'ordinary'
 for pkg,pattern in [('src/wago','^TestSharedScalar'),('src/core/compiler/backend/railshot/arm64','TestScalar|TestShared'),('src/core/compiler/backend/railshot/shared','TestScalar')]:
  name='arm64-'+pkg.split('/')[-1]+'-'+mode;b=str(p/(name+'.test'))
  if run(name+'-build',['go','test','-c',*tags,'-o',b,'./'+pkg],dict(GOOS='linux',GOARCH='arm64'))==0:run(name,[qemu,b,'-test.run='+pattern,'-test.v'])
 for goos in ['darwin','windows']:
  for arch in ['amd64','arm64']:
   for pkg in ['bench/suite','src/core/compiler/backend/railshot/'+arch]:
    name=goos+'-'+arch+'-'+pkg.split('/')[-1]+'-'+mode;run(name,['go','test','-c',*tags,'-o',str(p/(name+'.test')),'./'+pkg],dict(GOOS=goos,GOARCH=arch))
# Identical diagnostic harness and hashes on arm64, executed under emulation.
for rev,root in [('M',p.parent/'pr802-main'),('T',p.parent/'pr802-packed'),('P',r)]:
 b=str(p/(rev+'-arm64-diagnostic.test'));cmd=['go','test','-c','-tags=wago_codegenstats','-o',b,'./bench/suite']
 with(p/(rev+'-arm64-diagnostic-build.txt')).open('w')as out:res=subprocess.run(cmd,cwd=root,env=dict(env,GOOS='linux',GOARCH='arm64'),stdout=out,stderr=subprocess.STDOUT)
 runs.append(dict(name=rev+'-arm64-diagnostic-build',command=cmd,exit=res.returncode))
 if res.returncode==0:run(rev+'-arm64-diagnostic',[qemu,b,'-test.run=^TestSharingDiagnostic$','-test.v','-wago.corpus=tiny,fib_rec,many_funcs,xxhash,json-as'])
(p/'cross-checks.json').write_text(json.dumps(runs,indent=2))
