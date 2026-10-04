from pathlib import Path
import subprocess,os,time,json,hashlib
p=Path(__file__).parent;r=p.parent/'pr802-final';env=dict(os.environ,GOMAXPROCS='1',GOGC='100',GOMEMLIMIT='off');env.pop('WAGO_SHARED_SCALAR',None)
plan=[]
for kind in ['timing','long','workers','memory','memory-auto','runtime','runtime-auto','diagnostic']:
 for block in range(1,4):
  for seq,rev in enumerate(['M','B','R','P','P','R','B','M'],1):
   name=f'final-{kind}-block{block}-{seq}-{rev}';print(name,flush=True);runenv=dict(env)
   args=['taskset','-c','4',str(p/(rev+'-suite.test'))]
   if kind=='timing':args+=['-test.run=^$','-test.bench=^(BenchmarkSharing(Native|Full|Exec|ZeroLocals)|BenchmarkCompile|BenchmarkCompileFull|BenchmarkExec)$','-test.benchtime=200ms','-test.count=1','-wago.corpus=tiny,fib_rec,many_funcs,xxhash,json-as']
   elif kind=='long':args+=['-test.run=^$','-test.bench=^BenchmarkSharingExec$','-test.benchtime=1s','-test.count=1']
   elif kind=='workers':
    runenv['GOMAXPROCS']='4';args=['taskset','-c','2-5',str(p/(rev+'-suite.test')),'-test.run=^$','-test.bench=^BenchmarkCompileFullWorkers$/(tiny|many_funcs|json-as)/(p1|auto)$','-test.benchtime=200ms','-test.count=1','-wago.corpus=tiny,many_funcs,json-as']
   elif kind=='diagnostic':args=['taskset','-c','4',str(p/(rev+'-diagnostic.test')),'-test.run=^TestSharingDiagnostic$','-test.v','-wago.corpus=tiny,fib_rec,many_funcs,xxhash,json-as']
   elif kind.startswith('memory'):
    runenv['WAGO_SHARING_MEMORY']='1';args+=['-test.run=^TestSharing(Memory|MappedMemory)$','-test.v','-test.count=1']
   else:args=['taskset','-c','4',str(p/(rev+'-runtime-memory'))]
   if kind.endswith('-auto'):
    runenv.update(GOMAXPROCS='4',WAGO_SHARING_WORKERS='0');args[2]='2-5'
   args=['python3',str(p/'resource_wrapper.py'),str(p/(name+'.resource.json'))]+args
   begin=time.time()
   with(p/(name+'.txt')).open('w')as out:res=subprocess.run(args,cwd=r/'bench/suite',env=runenv,stdout=out,stderr=subprocess.STDOUT)
   plan.append(dict(kind=kind,block=block,sequence=seq,revision=rev,start=begin,elapsed=time.time()-begin,exit=res.returncode,command=args));(p/'final-measurement-runs.json').write_text(json.dumps(plan,indent=2))
   if res.returncode:raise SystemExit(f'{name} failed')
