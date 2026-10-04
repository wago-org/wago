from pathlib import Path
import subprocess,os,json
p=Path(__file__).parent;e=p.parent;r=e.parent/'pr802-final';env=dict(os.environ,GOCACHE=str(e/'go-cache'),GOFLAGS='-buildvcs=false');runs=[]
qemu='/home/jtenner/.codex/visualizations/2026/10/03/01a103ee-af5e-7f11-a74d-8c68fd8e53a4/wago-pr802-recovery/qemu/usr/bin/qemu-aarch64'
for checked in [False,True]:
 tags=['-tags=wago_regalloccheck']if checked else[];mode='checked'if checked else'ordinary';name='arm64-backend-'+mode;b=p/(name+'.test');cmd=['go','test','-c',*tags,'-o',str(b),'./src/core/compiler/backend/railshot/arm64']
 with(p/(name+'-build.txt')).open('w')as f:res=subprocess.run(cmd,cwd=r,env=dict(env,GOOS='linux',GOARCH='arm64'),stdout=f,stderr=subprocess.STDOUT)
 runs.append(dict(name=name+'-build',command=cmd,exit=res.returncode));print(name+'-build',res.returncode,flush=True)
 if res.returncode:raise SystemExit(1)
 cmd=[qemu,str(b),'-test.run=TestScalar|TestShared','-test.v']
 with(p/(name+'.txt')).open('w')as f:res=subprocess.run(cmd,cwd=r,env=env,stdout=f,stderr=subprocess.STDOUT)
 runs.append(dict(name=name,command=cmd,exit=res.returncode));print(name,res.returncode,flush=True)
 if res.returncode:raise SystemExit(1)
 for goos in ['darwin','windows']:
  for arch in ['amd64','arm64']:
   for pkg in ['bench/suite','src/core/compiler/backend/railshot/'+arch]:
    name=goos+'-'+arch+'-'+pkg.split('/')[-1]+'-'+mode;cmd=['go','test','-c',*tags,'-o',str(p/(name+'.test')),'./'+pkg]
    with(p/(name+'.txt')).open('w')as f:res=subprocess.run(cmd,cwd=r,env=dict(env,GOOS=goos,GOARCH=arch),stdout=f,stderr=subprocess.STDOUT)
    runs.append(dict(name=name,command=cmd,exit=res.returncode));(p/'cross-checks.json').write_text(json.dumps(runs,indent=2));print(name,res.returncode,flush=True)
    if res.returncode:raise SystemExit(1)
(p/'cross-checks.json').write_text(json.dumps(runs,indent=2))
