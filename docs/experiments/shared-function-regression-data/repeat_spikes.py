from pathlib import Path
import subprocess,os,json
p=Path(__file__).parent;e=p.parent;r=e.parent/'pr802-final/bench/suite';env=dict(os.environ,GOMAXPROCS='1',GOGC='100',GOMEMLIMIT='off');env.pop('WAGO_SHARED_SCALAR',None);runs=[]
for kind in ['execution','compile']:
 for block in range(1,4):
  for seq,rev in enumerate(['M','P','F','F','P','M'],1):
   name=f'repeat-{kind}-block{block}-{seq}-{rev}';b=(p if rev=='F'else e)/(rev+'-suite.test')
   pattern='^(BenchmarkSharingExec|BenchmarkExec)$/(join|tiny.add)$'if kind=='execution'else'^BenchmarkSharingFull$/fallback$'
   cmd=['taskset','-c','4',str(b),'-test.run=^$','-test.bench='+pattern,'-test.benchtime=1s','-test.count=1','-wago.corpus=tiny,many_funcs']
   print(name,flush=True)
   with(p/(name+'.txt')).open('w')as f:res=subprocess.run(cmd,cwd=r,env=env,stdout=f,stderr=subprocess.STDOUT)
   runs.append(dict(kind=kind,revision=rev,command=cmd,exit=res.returncode));(p/'repeat-runs.json').write_text(json.dumps(runs,indent=2))
   if res.returncode:raise SystemExit(1)
 for rev in ['M','P','F']:
  (p/'results'/(rev+'-repeat-'+kind+'.txt')).write_text('\n'.join(f.read_text()for f in sorted(p.glob('repeat-'+kind+'-block*-'+rev+'.txt'))))
 for pair in ['MF','PF']:
  with(p/'results'/(pair+'-repeat-'+kind+'.txt')).open('w')as f:subprocess.run(['benchstat',str(p/'results'/(pair[0]+'-repeat-'+kind+'.txt')),str(p/'results'/(pair[1]+'-repeat-'+kind+'.txt'))],stdout=f,check=True)
 if kind=='execution':
  for rev in ['M','P','F']:
   lines=[]
   for f in sorted(p.glob('long-block*-'+rev+'.txt'))+sorted(p.glob('repeat-execution-block*-'+rev+'.txt')):
    lines += [l for l in f.read_text().splitlines()if l.startswith(('BenchmarkSharingExec/join ','BenchmarkExec/tiny.add '))]
   (p/'results'/(rev+'-combined-spikes.txt')).write_text('\n'.join(lines)+'\n')
  for pair in ['MF','PF']:
   with(p/'results'/(pair+'-combined-spikes.txt')).open('w')as f:subprocess.run(['benchstat',str(p/'results'/(pair[0]+'-combined-spikes.txt')),str(p/'results'/(pair[1]+'-combined-spikes.txt'))],stdout=f,check=True)
