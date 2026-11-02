"""Refresh exact remaining scope; require follow-up execution census before credit."""
import pathlib,json,hashlib
E=pathlib.Path(__file__).resolve().parent;p=E/'final-summary.json';s=json.loads(p.read_text())
for row in s['required']:
 if row['verdict']!='PASS':continue
 if row['suite'].startswith('repair-census-'):
  assert (E/row['suite']/'census.json').exists(),row['suite']
 if row['suite'].startswith('tinygo-unsupported-global-'):
  cp=E/'tinygo-unsupported-global-census.json';cells=json.loads(cp.read_text()) if cp.exists() else []
  mode=row['suite'].removeprefix('tinygo-unsupported-global-')
  if not any(c['mode']==mode and c['actual_tinygo'] and not c['tag_adapter'] for c in cells):row['verdict']='INCOMPLETE_CENSUS'
remaining=[r for r in s['required'] if r['verdict']!='PASS'];done=[r['suite'] for r in s['required'] if r['verdict']=='PASS']
from collections import Counter
s['counts']=dict(Counter(r['verdict'] for r in s['required']));s['verdict']='FAILED / INCOMPLETE' if any(r['verdict']=='FAIL' for r in remaining) else 'INCOMPLETE' if remaining else 'PASS'
p.write_text(json.dumps(s,indent=2)+'\n')
(E/'remaining-gates.json').write_text(json.dumps({'source':s['source'],'remaining_count':len(remaining),'remaining':remaining,'completed':done},indent=2)+'\n')
for n,h in json.loads((E/'correction-drivers.json').read_text()).items():assert hashlib.sha256((E/n).read_bytes()).hexdigest()==h,n
print('required',len(s['required']),s['counts'])
