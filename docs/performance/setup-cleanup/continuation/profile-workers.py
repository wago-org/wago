from pathlib import Path
import os,subprocess
root=Path(__file__).resolve().parents[4];out=Path(__file__).resolve().parent;tmp=root/'.tmp/setup-cleanup-next'
env=dict(os.environ,GOMAXPROCS='16',GOGC='100',GOMEMLIMIT='off',GODEBUG='',WAGO_BOUNDS='signals')
for module in ['many_funcs','json-as','lua']:
 for stage in ['validate','backend']:
  for workers in [1,4]:
   name=f'worker-{module}-{stage}-{workers}'
   profile=tmp/f'{name}.alloc'
   cmd=['taskset','-c','0-15',str(tmp/'worker-owned.test'),'-test.run','^$','-test.bench',f'^BenchmarkWorkerLifecycleDiagnostic$/^{module}$/^callers=1$/^{stage}$/^raw$/^requested={workers}$','-test.benchtime','100x','-test.memprofile',str(profile),'-test.memprofilerate','1','-wago.bench.lifecycle','-wago.corpus',module]
   with (out/f'{name}-profile-run.txt').open('w') as f:
    f.write(repr(cmd)+'\n');f.flush();subprocess.run(cmd,cwd=root/'bench/suite',env=env,stdout=f,stderr=subprocess.STDOUT,check=True)
   for metric in ['alloc_space','alloc_objects']:
    with (out/f'{name}-{metric}.txt').open('w') as f: subprocess.run(['go','tool','pprof','-top','-sample_index',metric,str(tmp/'worker-owned.test'),str(profile)],stdout=f,check=True)
   with (out/f'{name}-sites.txt').open('w') as f: subprocess.run(['go','tool','pprof','-list','compileModuleParallel|newScratchWithStackCap|validateFunctionsParallel|funcValidator.*push|stack.*alloc','-sample_index','alloc_space',str(tmp/'worker-owned.test'),str(profile)],stdout=f,check=True)
   print(name,flush=True)
