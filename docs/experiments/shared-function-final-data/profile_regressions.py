from pathlib import Path
import json,os,subprocess
p=Path(__file__).parent;r=p.parent/'pr802-final/bench/suite';s=json.loads((p/'results/timing-summary.json').read_text());env=dict(os.environ,GOMAXPROCS='1',GOGC='100',GOMEMLIMIT='off');runs=[]
for name in ['SharingNative/small','SharingNative/pressure','SharingNative/large','SharingNative/many','SharingNative/large_then_small','SharingFull/deep']:
 a=s[name]['R']['ns/op']['median'];b=s[name]['P']['ns/op']['median'];m=s[name]['M']['ns/op']['median'];ratio=b/a-1
 if ratio<=.05 and b/m-1<=.05:continue
 group,workload=name.split('/')
 for rev in ['R','P']:
  fn=rev+'-'+group+'-'+workload;profile=p/(fn+'.cpu');cmd=['taskset','-c','4',str(p/(rev+'-suite.test')),'-test.run=^$','-test.bench=^Benchmark'+group+'$/^'+workload+'$','-test.benchtime=3s','-test.count=1','-test.cpuprofile='+str(profile)]
  with(p/(fn+'-profile.txt')).open('w')as out:res=subprocess.run(cmd,cwd=r,env=env,stdout=out,stderr=subprocess.STDOUT)
  runs.append(dict(benchmark=name,revision=rev,command=cmd,exit=res.returncode))
  with(p/(fn+'-cpu-top.txt')).open('w')as out:subprocess.run(['go','tool','pprof','-top','-nodecount=30',str(p/(rev+'-suite.test')),str(profile)],stdout=out,stderr=subprocess.STDOUT,check=True)
(p/'regression-profile-runs.json').write_text(json.dumps(runs,indent=2))
