from pathlib import Path
import subprocess,os,json
p=Path(__file__).parent;e=p.parent;r=e.parent/'pr802-final/bench/suite';runs=[];env=dict(os.environ,GOMAXPROCS='1',GOGC='100',GOMEMLIMIT='off');env.pop('WAGO_SHARED_SCALAR',None)
for block in range(1,4):
 for seq,rev in enumerate(['B','F','F','B'],1):
  name=f'helpers-block{block}-{seq}-{rev}';b=(p if rev=='F' else e)/(rev+'-suite.test')
  cmd=['taskset','-c','4',str(b),'-test.run=^$','-test.bench=^(BenchmarkSharing(Native|Full|Exec|ZeroLocals)|BenchmarkCompile|BenchmarkCompileFull|BenchmarkExec)$','-test.benchtime=200ms','-test.count=1','-wago.corpus=tiny,fib_rec,many_funcs,xxhash,json-as']
  print(name,flush=True)
  with(p/(name+'.txt')).open('w')as f:res=subprocess.run(cmd,cwd=r,env=env,stdout=f,stderr=subprocess.STDOUT)
  runs.append(dict(revision=rev,command=cmd,exit=res.returncode));(p/'helper-runs.json').write_text(json.dumps(runs,indent=2))
  if res.returncode:raise SystemExit(1)
for rev in ['B','F']:(p/'results'/(rev+'-helpers.txt')).write_text('\n'.join(f.read_text()for f in sorted(p.glob('helpers-block*-'+rev+'.txt'))))
for fmt in ['text','csv']:
 with(p/'results'/('BF-helpers.'+fmt)).open('w')as out:subprocess.run(['benchstat','-format='+fmt,str(p/'results/B-helpers.txt'),str(p/'results/F-helpers.txt')],stdout=out,check=True)
