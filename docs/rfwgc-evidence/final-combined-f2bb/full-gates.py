"""Sequential complete correctness recipes; stop on any nonzero gate."""
import run as q
import json,hashlib
assert json.loads((q.E/'early-summary.json').read_text())['failures']==[]
steps=[('repairs-v2.py',),('qualify.py','preflight'),('root-replay.py',),('run.py','supplement'),('run.py','spec'),('baseline-preflight.py',),('run.py','corpus'),('qualify.py','baseline'),('continuation.py',),('tooling-isolated.py',),('tooling-replay.py',),('target-families.py',),('target-repairs.py','--pin',q.M['pin'],'--qemu-package','/tmp/qemu-user.deb','--out',str(q.E/'qemu-repairs')),('tinygo-complete.py',)]
(q.E/'full-plan.json').write_text(json.dumps(steps,indent=2))
(q.E/'drivers-before.json').write_text(json.dumps({p.name:hashlib.sha256(p.read_bytes()).hexdigest() for p in q.E.iterdir() if p.suffix in ['.py','.sh']},indent=2))
for i,step in enumerate(steps):
 if q.run('full-step-'+str(i),['/usr/bin/python3',str(q.E/step[0]),*step[1:]],q.E.parents[2]):raise SystemExit(1)
