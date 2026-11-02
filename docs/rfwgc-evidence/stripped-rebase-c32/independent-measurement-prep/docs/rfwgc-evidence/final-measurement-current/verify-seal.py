#!/usr/bin/env python3
"""Read-only full integrity, scope and no-acquisition contract check."""
import pathlib,json,hashlib,os
H=pathlib.Path(__file__).resolve().parent
def sha(p):
 d=hashlib.sha256()
 with pathlib.Path(p).open('rb') as f:
  for b in iter(lambda:f.read(1048576),b''):d.update(b)
 return d.hexdigest()
s=json.loads((H/'seal-manifest.json').read_text());p=json.loads((H/'plan.json').read_text())
assert s['status']=='PREPARATION_ONLY' and s['candidate_valid'] is False and s['acquisition_authorized'] is False
assert len(s['cells'])==349 and len(p['cells'])==349 and s['plan_sha256']==sha(H/'plan.json')
assert all(sha(path)==digest for path,digest in s['files'].items())
assert all(os.readlink(path)==target for path,target in s['symlinks'].items())
for family in ['qualification','gates','detailed-census','shape-sizes']:
 rows=json.loads((H/family/'receipts.json').read_text())
 for r in rows:
  argv=r['argv'];assert not any(a.startswith(('-test.bench','-bench')) for a in argv)
  assert ('-c' in argv) or any(a.startswith('-test.run=') for a in argv),argv
  digest=r.get('log_sha256',r.get('sha256'));assert sha(H/r['log'])==digest
assert all(row['paired_leaf_match'] for row in s['cells'])
assert not s['unresolved_bindings'],s['unresolved_bindings']
print('PASS: full manifest identity, 349-cell scope, nonempty matched census, all-three-mode bindings, no benchmark execution and PREPARATION_ONLY gates')
