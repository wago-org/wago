from pathlib import Path
import subprocess,os,json
p=Path(__file__).parent;e=p.parent;r=e.parent/'pr802-final/bench/suite';runs=[]
for kind in ['memory','memory-auto','runtime','runtime-auto']:
 for block in range(1,4):
  for seq,rev in enumerate(['M','F','F','M'],1):
   name=f'no-thp-{kind}-block{block}-{seq}-{rev}';env=dict(os.environ,GOMAXPROCS='1',GOGC='100',GOMEMLIMIT='off');env.pop('WAGO_SHARED_SCALAR',None)
   b=(p if rev=='F'else e)/(rev+('-runtime-memory'if kind.startswith('runtime')else'-suite.test'))
   args=['taskset','-c','4','python3',str(p/'no_thp_exec.py'),str(b)]
   if kind.startswith('memory'):env['WAGO_SHARING_MEMORY']='1';args+=['-test.run=^TestSharing(Memory|MappedMemory)$','-test.v','-test.count=1']
   if kind.endswith('-auto'):env.update(GOMAXPROCS='4',WAGO_SHARING_WORKERS='0');args[2]='2-5'
   cmd=['python3',str(e/'resource_wrapper.py'),str(p/(name+'.resource.json'))]+args
   print(name,flush=True)
   with(p/(name+'.txt')).open('w')as f:res=subprocess.run(cmd,cwd=r,env=env,stdout=f,stderr=subprocess.STDOUT)
   runs.append(dict(revision=rev,kind=kind,command=cmd,exit=res.returncode));(p/'no-thp-runs.json').write_text(json.dumps(runs,indent=2))
   if res.returncode:raise SystemExit(name+' failed')
