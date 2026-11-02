"""Only deterministic build/CI-policy reproductions after qualification stop."""
import run as q
import subprocess,json,hashlib
rows=[]
for mode in ['ordinary','wagodebug','wago_regalloccheck']:
 for label,pkg,args in [('probe-package','./docs/rfwgc-evidence/final-review-design/probes',['-run','^$']),('ci-policy','./tests/integration/cipolicy',['-run','^TestAggregateCIRequiresEveryWorkflowJob$'])]:
  out=q.E/('reproduce-'+label+'-'+mode);out.mkdir()
  cmd=[q.G+'/bin/go','test','-json','-count=1',*(['-tags='+mode] if mode!='ordinary' else []),*args,pkg]
  (out/'command.json').write_text(json.dumps(dict(source=q.M['pin'],argv=cmd,cwd=str(q.S),purpose='non-runtime deterministic defect reproduction after stop'),indent=2))
  with (out/'raw.log').open('w') as f:r=subprocess.run(cmd,cwd=q.S,env=q.env,stdout=f,stderr=subprocess.STDOUT)
  row=dict(mode=mode,gate=label,exit=r.returncode,verdict='FAIL' if r.returncode else 'PASS',raw_sha256=hashlib.sha256((out/'raw.log').read_bytes()).hexdigest())
  (out/'result.json').write_text(json.dumps(row,indent=2));rows.append(row);print(row,flush=True)
(q.E/'defect-reproductions.json').write_text(json.dumps(rows,indent=2))
