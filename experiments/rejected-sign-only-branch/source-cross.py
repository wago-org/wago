import json,os,subprocess
from pathlib import Path
states=[('baseline','/tmp/paired-next-20261011-base','0'),('source-off','/tmp/paired-sign-only-if-mitigated','0'),('candidate-on','/tmp/paired-sign-only-if-mitigated','1')]
with Path('/tmp/sign-only-if-source-cross.jsonl').open('w') as out:
 for phase in ['compile','exec']:
  for pair in range(4):
   order=states if pair%2==0 else list(reversed(states))
   for label,binary,flag in order:
    env=dict(os.environ,WAGO_SHARED_SCALAR='0',WAGO_ARM64_EXPERIMENT_SIGN_ONLY_IF=flag)
    args=[binary,'-option','loop-memory-base','-phase',phase,'-workloads','compiler-register-allocation,vision-dilation','-rounds','1','-budget','200ms']
    run=subprocess.run(args,env=env,text=True,capture_output=True,check=True)
    for line in run.stdout.splitlines():
     if line.startswith('{'):
      row=json.loads(line)
      if row['on']:
       row.update(build=label,pair=pair,protocol='four reversed process triples; common loop-memory-base ON')
       out.write(json.dumps(row)+'\n');out.flush()
