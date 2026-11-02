"""One finite batch per invocation. Never automatically schedule the next batch."""
import run as q
import sys,runpy,json,pathlib,hashlib,gzip
batches={
 'root':['recovery-remaining-root.py'],
 'supplement':['run.py','supplement'],
 'spec':['run.py','spec'],
 'baseline-preflight':['baseline-preflight.py'],
 'candidate-corpus':['run.py','corpus'],
 'baseline-corpus':['qualify.py','baseline'],
 'signals':['special.py'],
 'fuzz':['lanes.py','fuzz'],
 'extended-fuzz':['fuzz-extended.py'],
 'hardening':['lanes.py','hardening'],
 'ancillary':['ancillary.py'],
 'diagnostics':['qualify.py','diagnostics'],
 'graphs':['qualify.py','graphs'],
 'tooling':['tooling-isolated.py'],
 'tooling-replay':['tooling-replay.py'],
 'targets':['target-families.py'],
 'target-repairs':['target-repairs.py','--pin',q.M['pin'],'--qemu-package','/tmp/qemu-user.deb','--out',str(q.E/'qemu-repairs')],
 'tinygo':['tinygo-complete.py'],
}
assert len(sys.argv)==2 and sys.argv[1] in batches, 'Choose one batch: '+', '.join(batches)
assert not (q.E/'SAFETY-STOP.json').exists(),'Resolve safety stop before any execution'
if sys.argv[1]=='baseline-corpus':
 d=q.E/'baseline-canonical-checked-preflight';r=json.loads((d/'result.json').read_text())
 assert r['exit']==0 and r['source']==json.loads((q.E/'baseline.json').read_text())['pin']
 assert hashlib.sha256(gzip.decompress((d/'raw.log.gz').read_bytes())).hexdigest()==r['raw_sha256']
step=batches[sys.argv[1]];target=q.E/step[0]
if step[0]=='run.py':target=q.E/'run-v4.py'
sys.argv=[str(target),*step[1:]]
runpy.run_path(str(target),run_name='__main__')
