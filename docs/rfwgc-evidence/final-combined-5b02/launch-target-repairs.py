"""Launch the frozen original target repair driver with explicit owned environment."""
import run as q
q.run('target-repairs-original',['/usr/bin/python3',str(q.E/'target-repairs.py'),'--pin',q.M['pin'],'--qemu-package','/tmp/qemu-user.deb','--out',str(q.E/'qemu-repairs')],q.E.parents[2])
