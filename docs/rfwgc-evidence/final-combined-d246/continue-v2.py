import run as q
import json
for i,step in enumerate(json.loads((q.E/'full-plan.json').read_text())[2:],2):
 if q.run('continued-step-'+str(i),['/usr/bin/python3',str(q.E/step[0]),*step[1:]],q.E.parents[2]):raise SystemExit(1)
