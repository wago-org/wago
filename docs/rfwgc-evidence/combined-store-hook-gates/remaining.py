#!/usr/bin/env python3
import run as q
import sys
for group in ['hardening','evacuation','fuzz']:
 if q.run('driver-'+group,['python3',str(q.E/'lanes.py'),group],q.E.parents[2]):sys.exit(1)
if q.run('driver-signals-guard-corpus',['python3',str(q.E/'special.py')],q.E.parents[2]):sys.exit(1)
