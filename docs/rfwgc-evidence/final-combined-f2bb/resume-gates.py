import run as q
import json
steps=[('root-replay.py',),*map(tuple,json.loads((q.E/'full-plan.json').read_text())[3:])]
for i,step in enumerate(steps):
 if q.run('recovery-step-'+str(i),['/usr/bin/python3',str(q.E/step[0]),*step[1:]],q.E.parents[2]):raise SystemExit(1)
