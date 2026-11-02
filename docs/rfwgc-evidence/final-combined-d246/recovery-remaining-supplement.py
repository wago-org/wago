"""Resume the finite supplement batch, reusing only identical verified commands."""
import run as q
import json,hashlib,gzip,sys
original=q.run
def resume(name,cmd,cwd=q.S,extra=None):
 d=q.E/name
 if not d.exists():return original(name,cmd,cwd,extra)
 c=json.loads((d/'command.json').read_text());r=json.loads((d/'result.json').read_text())
 assert c['argv']==cmd and c['cwd']==str(cwd), 'Command changed: '+name
 assert r['exit']==0 and r['source']==q.M['pin'] and c['source']==q.M['pin']
 assert r['raw_sha256']==hashlib.sha256(gzip.decompress((d/'raw.log.gz').read_bytes())).hexdigest()
 assert r['driver_unchanged'] and c['driver_sha256']==hashlib.sha256((q.E/'run-v4.py').read_bytes()).hexdigest()
 print('VERIFIED EXISTING',name,flush=True);return 0
q.run=resume
for mode in ['ordinary','wagodebug','wago_regalloccheck']:
 for label,pkgs,args in [('collector',['./src/core/runtime/gc/...'],[]),('race',['./src/core/runtime/gc/...','./src/core/runtime','./src/wago','./tests/integration/...'],['-race']),('guard',['./src/core/runtime','./src/wago'],[])]:
  tags=mode+(',wago_guardpage' if label=='guard' else '') if mode!='ordinary' else ('wago_guardpage' if label=='guard' else mode)
  if q.test(label+'-'+mode,pkgs,tags,['-failfast',*args]):sys.exit(1)
