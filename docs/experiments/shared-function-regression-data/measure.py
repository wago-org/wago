from pathlib import Path
import subprocess,os,json,time,hashlib
p=Path(__file__).parent;e=p.parent;r=e.parent/'pr802-final';env=dict(os.environ,GOMAXPROCS='1',GOGC='100',GOMEMLIMIT='off');env.pop('WAGO_SHARED_SCALAR',None);runs=[]
for kind in ['memory','memory-auto','runtime','runtime-auto','timing','long','workers','diagnostic']:
 order=['M','P','F','F','P','M']
 if kind in ['memory','memory-auto']:order=['M','P','P0','F','F0','F0','F','P0','P','M']
 for block in range(1,4):
  for seq,rev in enumerate(order,1):
   base=rev[0];b=(p if base=='F' else e)/(base+'-suite.test');name=f'{kind}-block{block}-{seq}-{rev}';runenv=dict(env)
   if rev.endswith('0'):runenv['WAGO_SHARED_SCALAR']='0'
   args=['taskset','-c','4',str(b)]
   if kind=='timing':args+=['-test.run=^$','-test.bench=^(BenchmarkSharing(Native|Full|Exec|ZeroLocals)|BenchmarkCompile|BenchmarkCompileFull|BenchmarkExec)$','-test.benchtime=200ms','-test.count=1','-wago.corpus=tiny,fib_rec,many_funcs,xxhash,json-as']
   elif kind=='long':args+=['-test.run=^$','-test.bench=^(BenchmarkSharingExec|BenchmarkExec)$/(small|pressure|join|large|deep|locals|many|large_then_small|fallback|tiny.add|many_funcs.run)$','-test.benchtime=1s','-test.count=1','-wago.corpus=tiny,many_funcs']
   elif kind=='workers':
    runenv['GOMAXPROCS']='4';args=['taskset','-c','2-5',str(b),'-test.run=^$','-test.bench=^BenchmarkCompileFullWorkers$/(tiny|many_funcs|json-as)/(p1|auto)$','-test.benchtime=200ms','-test.count=1','-wago.corpus=tiny,many_funcs,json-as']
   elif kind=='diagnostic':args=['taskset','-c','4',str((p if base=='F' else e)/(base+'-diagnostic.test')),'-test.run=^TestSharingDiagnostic$','-test.v','-wago.corpus=tiny,fib_rec,many_funcs,xxhash,json-as']
   elif kind.startswith('memory'):
    runenv['WAGO_SHARING_MEMORY']='1';args+=['-test.run=^TestSharing(Memory|MappedMemory)$','-test.v','-test.count=1']
   else:args=['taskset','-c','4',str((p if base=='F' else e)/(base+'-runtime-memory'))]
   if kind.endswith('-auto'):runenv.update(GOMAXPROCS='4',WAGO_SHARING_WORKERS='0');args[2]='2-5'
   args=['python3',str(e/'resource_wrapper.py'),str(p/(name+'.resource.json'))]+args
   start=time.time();print(name,flush=True)
   with(p/(name+'.txt')).open('w')as f:res=subprocess.run(args,cwd=r/'bench/suite',env=runenv,stdout=f,stderr=subprocess.STDOUT)
   runs.append(dict(kind=kind,block=block,sequence=seq,revision=rev,start=start,elapsed=time.time()-start,exit=res.returncode,command=args,shared=runenv.get('WAGO_SHARED_SCALAR','default')));(p/'measurement-runs.json').write_text(json.dumps(runs,indent=2))
   if res.returncode:raise SystemExit(name+' failed')
