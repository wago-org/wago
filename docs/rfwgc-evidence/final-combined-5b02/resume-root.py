"""Resume root batch after classification pause, verifying prior commands before reuse."""
import run as q
import json,gzip,hashlib,runpy
original=q.run
reused=[]
def resume(name,cmd,cwd=q.S,extra=None):
 d=q.E/name
 if not d.exists():return original(name,cmd,cwd,extra)
 c=json.loads((d/'command.json').read_text());r=json.loads((d/'result.json').read_text())
 assert c['source']==r['source']==q.M['pin'] and r['exit']==0 and r['driver_unchanged']
 assert c['argv']==cmd and c['cwd']==str(cwd)
 assert c['driver_sha256']==hashlib.sha256((q.E/'run.py').read_bytes()).hexdigest()
 assert r['raw_sha256']==hashlib.sha256(gzip.decompress((d/'raw.log.gz').read_bytes())).hexdigest()
 expected=q.env|(extra or {})
 for k in expected:
  if k.startswith('WAGO_') or k in ['GOROOT','GOTOOLCHAIN','GOFLAGS','GOENV','GOCACHE','XDG_CACHE_HOME','TMPDIR']:assert c['environment'].get(k)==expected[k],(name,k)
 reused.append(dict(suite=name,raw_sha256=r['raw_sha256'],command_identical=True));(q.E/'root-resume-reuse.json').write_text(json.dumps(reused,indent=2)+'\n')
 print('VERIFIED EXISTING',name,flush=True);return 0
q.run=resume
runpy.run_path(str(q.E/'root-replay.py'),run_name='__main__')
