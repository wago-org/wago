"""Finite remaining host batches, stop at first nonzero exit; no duplicate retry."""
import pathlib,subprocess,json,os,hashlib
E=pathlib.Path(__file__).resolve().parent;steps=[['lanes.py','fuzz'],['fuzz-extended.py'],['lanes.py','hardening'],['ancillary.py'],['qualify.py','diagnostics'],['qualify.py','graphs']]
record={'driver_sha256':hashlib.sha256(pathlib.Path(__file__).read_bytes()).hexdigest(),'pid':os.getpid(),'steps':steps,'completed':[]}
for step in steps:
 assert not (E/'SAFETY-STOP.json').exists() and not (E/'CHECKPOINT.json').exists()
 argv=['/usr/bin/python3',str(E/'bootstrap.py'),str(E/step[0]),*step[1:]]
 record['current']=argv;(E/'remaining-host-batch-state.json').write_text(json.dumps(record,indent=2)+'\n')
 print('BEGIN',step,flush=True);r=subprocess.run(argv,cwd=E.parents[2]);print('END',step,r.returncode,flush=True)
 record['completed'].append({'argv':argv,'exit':r.returncode});record['current']=None
 (E/'remaining-host-batch-state.json').write_text(json.dumps(record,indent=2)+'\n')
 if r.returncode:raise SystemExit(r.returncode)
