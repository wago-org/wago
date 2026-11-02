#!/usr/bin/env python3
"""Summarize immutable correctness evidence; never executes test workloads."""
import hashlib,json,pathlib
root=pathlib.Path(__file__).resolve().parent
summary={}
for name in ['integrated-ae64b76c1','targets-ae64b76c1','integrated-final','targets-final','integrated-verified','targets-verified','integrated-qualification','targets-qualification','integrated-receipt-final','targets-receipt-final']:
 d=root/name
 rows=json.loads((d/'results.json').read_text())
 source=json.loads((d/('provenance.json' if name.startswith('integrated') else 'source.json')).read_text())
 final=json.loads((d/'driver-final.json').read_text())
 commands=[]
 for row in rows:
  log=d/(row['label']+'.log')
  text=log.read_text()
  commands.append(dict(label=row['label'],exit=row['exit'],top_level_pass=sum(x.startswith('--- PASS:') for x in text.splitlines()),skip=row['skip'],fail=row['fail']))
 summary[name]=dict(pin=source['pin'],commands=commands,driver=final)
(root/'summary.json').write_text(json.dumps(summary,indent=2)+'\n')
files=sorted(p for p in root.rglob('*') if p.is_file() and p.name!='SHA256SUMS')
(root/'SHA256SUMS').write_text(''.join(hashlib.sha256(p.read_bytes()).hexdigest()+'  '+str(p.relative_to(root))+'\n' for p in files))
