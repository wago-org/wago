import run as q
import json,hashlib,pathlib
q.env['GOWORK']='off'
(q.E/'early-driver-before.json').write_text(json.dumps({p.name:hashlib.sha256(p.read_bytes()).hexdigest() for p in q.E.glob('*.py')},indent=2))
failures=[]
for mode in ['ordinary','wagodebug','wago_regalloccheck']:
 tags=[] if mode=='ordinary' else ['-tags='+mode]
 for name,cmd in [('discovery',[q.G+'/bin/go','list','-json',*tags,'./...']),('compile',[q.G+'/bin/go','test','-json','-count=1','-run=^$','-timeout=10m',*tags,'./...']),('cipolicy',[q.G+'/bin/go','test','-json','-count=1','-timeout=10m',*tags,'./tests/integration/cipolicy'])]:
  gate='early-'+name+'-'+mode
  if q.run(gate,cmd):failures.append(gate)
(q.E/'early-summary.json').write_text(json.dumps({'failures':failures},indent=2))
raise SystemExit(bool(failures))
