# Resume only terminal, hash-verified successes; never overwrite attempts.
import run as q
import json,hashlib,gzip
original=q.run
def resume(name,cmd,cwd=q.S,extra=None):
 d=q.E/name
 if not d.exists():return original(name,cmd,cwd,extra)
 c=json.loads((d/'command.json').read_text());r=json.loads((d/'result.json').read_text())
 raw=gzip.decompress((d/'raw.log.gz').read_bytes())
 assert r['exit']==0 and r['source']==q.M['pin'] and c['source']==q.M['pin']
 assert r['raw_sha256']==hashlib.sha256(raw).hexdigest() and r['driver_unchanged']
 assert c['driver_sha256']==hashlib.sha256((q.E/'run-v4.py').read_bytes()).hexdigest()
 print('VERIFIED EXISTING',name,flush=True);return 0
q.run=resume
import run as q
import sys
for m in ['ordinary','wagodebug','wago_regalloccheck']:
 if m!='ordinary' and q.test('full-root-replay-'+m,['./...'],m,['-failfast']):sys.exit(1)
 for name,pkg,tags,cwd in [('cli',['./cli/...'],'wago_runtime'+('' if m=='ordinary' else ','+m),q.S),('installer',['./...'],m,q.S/'cli/wago-installer'),('nested',['./...'],m,q.S/'bench')]:
  if q.test(name+'-'+m,pkg,tags,['-failfast'],cwd):sys.exit(1)
