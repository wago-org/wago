import run as q
steps=[('special.py',),('lanes.py','fuzz'),('fuzz-extended.py',),('lanes.py','hardening'),('ancillary.py',),('qualify.py','diagnostics'),('qualify.py','graphs')]
for i,step in enumerate(steps):
 if q.run('continuation-'+str(i),['/usr/bin/python3',str(q.E/step[0]),*step[1:]],q.E.parents[2]):raise SystemExit(1)
