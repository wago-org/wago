from pathlib import Path
import os,subprocess,json
p=Path(__file__).parent;e=p.parent;r=e.parent/'pr802-final/bench/suite';env=dict(os.environ,GOMAXPROCS='1',GOGC='100',GOMEMLIMIT='off');env.pop('WAGO_SHARED_SCALAR',None);runs=[]
for block in range(1,4):
 for seq,policy in enumerate(['normal','no-thp','no-thp','normal'],1):
  name=f'policy-exec-block{block}-{seq}-{policy}';cmd=['taskset','-c','4']
  if policy=='no-thp':cmd+=['python3',str(p/'no_thp_exec.py')]
  cmd += [str(p/'F-suite.test'),'-test.run=^$','-test.bench=^(BenchmarkSharingExec|BenchmarkExec)$','-test.benchtime=200ms','-test.count=1','-wago.corpus=tiny,fib_rec,many_funcs,xxhash,json-as']
  print(name,flush=True)
  with(p/(name+'.txt')).open('w')as f:res=subprocess.run(cmd,cwd=r,env=env,stdout=f,stderr=subprocess.STDOUT)
  runs.append(dict(policy=policy,command=cmd,exit=res.returncode));(p/'policy-execution-runs.json').write_text(json.dumps(runs,indent=2))
  if res.returncode:raise SystemExit(1)
for policy in ['normal','no-thp']:(p/'results'/(policy+'-execution.txt')).write_text('\n'.join(f.read_text()for f in sorted(p.glob('policy-exec-block*-'+policy+'.txt'))))
with(p/'results/policy-execution.txt').open('w')as f:subprocess.run(['benchstat',str(p/'results/normal-execution.txt'),str(p/'results/no-thp-execution.txt')],stdout=f,check=True)
