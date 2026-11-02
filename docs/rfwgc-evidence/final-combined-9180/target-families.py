import run as q
for lane in ['collector','facade','guest','h6','rv64-controls']:
 if q.run('target-family-'+lane,['/usr/bin/python3',str(q.E/('qemu-'+lane+'.py')),'--pin',q.M['pin'],'--qemu-package','/tmp/qemu-user.deb','--out',str(q.E/('qemu-'+lane))],q.E.parents[2]):raise SystemExit(1)
