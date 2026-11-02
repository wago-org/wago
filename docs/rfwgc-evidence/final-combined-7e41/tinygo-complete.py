import run as q
for i,step in enumerate([('batch.py','tinygo'),('tinygo-repairs.py',)]):
 if q.run('tinygo-complete-'+str(i),['/usr/bin/python3',str(q.E/step[0]),*step[1:]],q.E.parents[2]):raise SystemExit(1)
