import run as q
import sys,json,pathlib,subprocess
lane=sys.argv[1]
steps={
'host':[('extra.py','root'),('run.py','supplement'),('lanes.py','hardening'),('ancillary.py',)],
'corpus':[('run.py','corpus'),('qualify.py','baseline'),('qualify.py','graphs'),('qualify.py','diagnostics')],
'spec':[('run.py','spec'),('special.py',),('lanes.py','fuzz')],
'platform':[('qemu-collector.py','--pin',q.M['pin'],'--qemu-package','/tmp/qemu-user.deb','--out',str(q.E/'qemu-collector')),('qemu-guest.py','--pin',q.M['pin'],'--qemu-package','/tmp/qemu-user.deb','--out',str(q.E/'qemu-guest'))],
 'tooling':[('tooling-isolated.py',),('tooling-replay.py',)],
 'tinygo':[('tinygo-34.sh',),('qualify.py','tinygo-extra')]
}[lane]
for i,step in enumerate(steps):
 name='batch-'+lane+'-'+str(i)
 ev={'WAGO_QUALIFICATION_HEAD':q.M['pin'],'WAGO_EVIDENCE_DIR':str(q.E/'tinygo-34')}
 cmd=['bash' if step[0].endswith('.sh') else 'python3',str(q.E/step[0]),*step[1:]]
 if q.run(name,cmd,q.E.parents[2],ev):raise SystemExit(1)
