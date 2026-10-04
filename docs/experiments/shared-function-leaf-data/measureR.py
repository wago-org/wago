from pathlib import Path
import subprocess,os,json,time,hashlib
p=Path(__file__).resolve().parent;e=p.parent;root=e.parent/'pr802-final'
env=dict(os.environ,GOMAXPROCS='1',GOGC='100',GOMEMLIMIT='off',CGO_ENABLED='0',GOAMD64='v1',GOARM64='v8.0');env.pop('WAGO_SHARED_SCALAR',None)
runs=[]
for kind in ['timing','long','workers','memory','memory-auto','runtime','runtime-auto']:
 for block in range(1,4):
  for seq,rev in enumerate(['A','C','R','R','C','A'],1):
   name=f'final-{kind}-block{block}-{seq}-{rev}';runenv=dict(env);binary=p/(rev+'-suite.test');args=['taskset','-c','4',str(binary)]
   if kind=='timing':args+=['-test.run=^$','-test.bench=^(BenchmarkSharing(Native|Full|Exec|ZeroLocals)|BenchmarkCompile|BenchmarkCompileFull|BenchmarkExec)$','-test.benchtime=200ms','-test.count=1','-wago.corpus=tiny,fib_rec,many_funcs,xxhash,json-as']
   elif kind=='long':args+=['-test.run=^$','-test.bench=^(BenchmarkSharingExec|BenchmarkExec)$/(small|pressure|join|locals|many|tiny.add|many_funcs.run)$','-test.benchtime=1s','-test.count=1','-wago.corpus=tiny,many_funcs']
   elif kind=='workers':
    runenv['GOMAXPROCS']='4';args=['taskset','-c','2-5',str(binary),'-test.run=^$','-test.bench=^BenchmarkCompileFullWorkers$/(tiny|many_funcs|json-as)/(p1|auto)$','-test.benchtime=200ms','-test.count=1','-wago.corpus=tiny,many_funcs,json-as']
   elif kind.startswith('memory'):
    runenv['WAGO_SHARING_MEMORY']='1';args+=['-test.run=^TestSharing(Memory|MappedMemory)$','-test.v','-test.count=1']
   else:args=['taskset','-c','4',str(p/(rev+'-runtime-memory'))]
   if kind.endswith('-auto'):runenv.update(GOMAXPROCS='4',WAGO_SHARING_WORKERS='0');args[2]='2-5'
   args=['python3',str(e/'resource_wrapper.py'),str(p/(name+'.resource.json'))]+args
   start=time.time();print(name,flush=True)
   with(p/(name+'.txt')).open('w')as f:res=subprocess.run(args,cwd=root/'bench/suite',env=runenv,stdout=f,stderr=subprocess.STDOUT)
   runs.append(dict(kind=kind,block=block,sequence=seq,revision=rev,start=start,elapsed=time.time()-start,exit=res.returncode,command=args,environment={k:runenv[k]for k in ['GOMAXPROCS','GOGC','GOMEMLIMIT','CGO_ENABLED','GOAMD64','GOARM64']},shared=runenv.get('WAGO_SHARED_SCALAR','default')));(p/'final-measurement-runs.json').write_text(json.dumps(runs,indent=2))
   if res.returncode:raise SystemExit(name+' failed')
print('MEASURED',flush=True)
