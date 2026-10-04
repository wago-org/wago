from pathlib import Path
import subprocess,os,json
p=Path(__file__).parent;r=p.parent/'pr802-final/bench/suite';env=dict(os.environ,GOMAXPROCS='1',GOGC='100',GOMEMLIMIT='off');runs=[]
for block in range(1,4):
 for seq,rev in enumerate(['Z','T','P','P','T','Z'],1):
  name=f'stage-repeat-block{block}-{seq}-{rev}';cmd=['taskset','-c','4',str(p/(rev+'-suite.test')),'-test.run=^$','-test.bench=^BenchmarkSharingNative$/(small|pressure|large|many|deep|locals)$','-test.benchtime=200ms','-test.count=1']
  with(p/(name+'.txt')).open('w')as f:res=subprocess.run(cmd,cwd=r,env=env,stdout=f,stderr=subprocess.STDOUT)
  print(name,res.returncode,flush=True);runs.append(dict(revision=rev,command=cmd,exit=res.returncode));(p/'stage-repeat-runs.json').write_text(json.dumps(runs,indent=2))
for rev in ['Z','T','P']:(p/'results'/(rev+'-stage-repeat.txt')).write_text('\n'.join(f.read_text()for f in sorted(p.glob('stage-repeat-block*-'+rev+'.txt'))))
for pair in ['ZT','TP','ZP']:
 with(p/'results'/(pair+'-stage-repeat.txt')).open('w')as out:subprocess.run(['benchstat',str(p/'results'/(pair[0]+'-stage-repeat.txt')),str(p/'results'/(pair[1]+'-stage-repeat.txt'))],stdout=out,check=True)
