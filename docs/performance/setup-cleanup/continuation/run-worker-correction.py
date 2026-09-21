from pathlib import Path
import os,subprocess,json,hashlib
root=Path(__file__).resolve().parents[4]
out=Path(__file__).resolve().parent
binary=root/'.tmp/setup-cleanup-next/diagnostic.test'
env=dict(os.environ,GOMAXPROCS='16',GOGC='100',GOMEMLIMIT='off',GODEBUG='',WAGO_BOUNDS='signals')
for sample in range(1,11):
 for path in (['raw','policy'] if sample%2 else ['policy','raw']):
  pattern=f'^BenchmarkWorkerLifecycleDiagnostic$/^(tiny|many_funcs|json-as|lua)$/^callers=(1|16)$/^(validate|backend)$/^{path}$/^requested=(0|1)$'
  cmd=['taskset','-c','0-15',str(binary),'-test.run','^$','-test.bench',pattern,'-test.count','1','-test.benchtime','100ms','-test.benchmem','-wago.bench.lifecycle','-wago.corpus','tiny,many_funcs,json-as,lua']
  with (out/f'workers-corrected-{path}.txt').open('a') as f:
   f.write(f'# sample {sample}; {cmd!r}\n');f.flush()
   subprocess.run(cmd,cwd=root/'bench/suite',env=env,stdout=f,stderr=subprocess.STDOUT,check=True)
  print(sample,path,flush=True)
