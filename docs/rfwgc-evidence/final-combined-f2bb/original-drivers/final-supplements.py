import run as q
for name,args in [('hardening',['lanes.py','hardening']),('ancillary',['ancillary.py']),('extended-fuzz',['fuzz-extended.py'])]:
 if q.run('final-'+name,['python3',str(q.E/args[0]),*args[1:]],q.E.parents[2]):raise SystemExit(1)
