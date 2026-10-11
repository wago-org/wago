import json,os,subprocess
from pathlib import Path
assert Path('/tmp/paired-hour-base-c74d').exists()
with open('experiments/arm64-parity/equality-countdown-offcost.jsonl','w') as out:
 for name in ['bio-lcs','grid-pathfinding','compiler-register-allocation','geo-point-in-polygon']:
  for round in range(8):
   for on in ([False,True] if round%2==0 else [True,False]):
    binary='/tmp/paired-equality-countdown-restricted' if on else '/tmp/paired-hour-base-c74d'
    command=[binary,'-option','regional-memory-read','-workloads',name,'-phase','compile','-rounds','1','-budget','150ms']
    result=subprocess.run(command,check=True,stdout=subprocess.PIPE,text=True,env=dict(os.environ,WAGO_SHARED_SCALAR='0',WAGO_ARM64_EXPERIMENT_EQUALITY_COUNTDOWN='0'))
    rows=[json.loads(line) for line in result.stdout.splitlines() if line.startswith('{')]
    row=next(row for row in rows if not row['on'])
    row.update(on=on,round=round,source=binary)
    out.write(json.dumps(row)+'\n');out.flush()
