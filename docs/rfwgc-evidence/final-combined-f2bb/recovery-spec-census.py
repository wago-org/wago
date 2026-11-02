"""Exact totals and zero-gap execution in every explicitly configured spec lane."""
import pathlib,json,gzip,re,hashlib
E=pathlib.Path(__file__).resolve().parent;D=hashlib.sha256(pathlib.Path(__file__).read_bytes()).hexdigest();pin=json.loads((E/'source.json').read_text())['pin'];rows=[]
for lane,version,modules,assertions in [('spec-v1','1.0',629,16026),('spec-v2','2.0',1600,48331),('spec-v3','3.0',2226,58238),('spec-v3-signals','3.0',2226,58238)]:
 for mode in ['ordinary','wagodebug','wago_regalloccheck']:
  d=E/(lane+'-'+mode);c=json.loads((d/'command.json').read_text());r=json.loads((d/'result.json').read_text());assert r['exit']==0 and c['source']==r['source']==pin
  assert c['environment']['WAGO_SPEC_REQUIRE_ZERO_GAPS']=='1' and c['environment']['WAGO_SPEC_VERSION']==version
  totals=[]
  for l in gzip.open(d/'raw.log.gz','rt'):
   try:x=json.loads(l)
   except ValueError:continue
   s=x.get('Output','')
   if 'TOTAL[' in s:totals.append(s.strip())
  assert len(totals)==1;line=totals[0]
  assert f'modules passed={modules} failed=0 skipped=0' in line and f'assertions passed={assertions} failed=0 skipped=0' in line
  gaps=dict(re.findall(r'([a-z-]+)=(\d+)',line.split('gaps ',1)[1]));assert len(gaps)==7 and all(v=='0' for v in gaps.values())
  rows.append(dict(suite=d.name,modules=modules,assertions=assertions,gaps=gaps,total=line))
assert D==hashlib.sha256(pathlib.Path(__file__).read_bytes()).hexdigest()
(E/'recovery-spec-census.json').write_text(json.dumps(dict(source=pin,driver_sha256=D,driver_unchanged=True,rows=rows),indent=2));print('PASS: exact totals, zero skips and zero gaps in',len(rows),'spec lanes')
