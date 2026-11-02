#!/usr/bin/env python3
"""Static correctness checks. Does not execute source/test/benchmark commands."""
import hashlib,json,pathlib,re,subprocess
H=pathlib.Path(__file__).resolve().parent;R=H.parents[2]
p=json.loads((H/'plan.json').read_text());assert len(p['cells'])==316
assert len({c['id'] for c in p['cells']})==316
assert p['status']=='PREPARATION_ONLY'
assert p['admission_c']==60 and p['discard_c']==85
assert p['pins']['port']=='5345bd365c13963f1bf60e26bb80789ab9ead4cc'
for c in p['cells']:
 for side in ['A','B']:
  s=c[side];assert s['pin'] in p['pins']
  if s['pin']=='baseline':
   assert s['env'].get('RFW_REGION','Off')=='Off'
   assert s['env'].get('FM_WORKERS','1')=='1'
   assert s['env'].get('FM_SHIFT','0')=='0'
   assert c['kind']!='test-cycle', 'unsupported ownership/foreign baseline'
  if c['target'].startswith('tinygo'):
   assert s['pin']=='port' and s['env'].get('FM_WORKERS','1') in ['0','1']
 if c['id'].startswith('owners/'):
  assert c['A']['selector'].endswith('/^0$') and c['B']['selector'].endswith('/^6$')
for path,digest in json.loads((H/'inputs.json').read_text()).items():assert hashlib.sha256((R/path).read_bytes()).hexdigest()==digest,path
census=json.loads((H/'census.json').read_text());assert len(census)==128
assert len({r['id'] for r in census})==128 and all(r['match'] for r in census)
# Test top-level names must exist in the pinned package or declared overlay.
for label,pin in p['pins'].items():
 if label=='evidence':continue
 paths=subprocess.check_output(['git','ls-tree','-r','--name-only',pin,'src/wago','src/core/runtime'],cwd=R,text=True).splitlines()
 names=set()
 for path in paths:
  if path.endswith('_test.go'):
   raw=subprocess.check_output(['git','show',pin+':'+path],cwd=R,text=True)
   names.update(re.findall(r'func (Test\w+)\(',raw))
 names.update(['TestFinalMeasurementIdleStacks','TestFinalMeasurementEvacuation'])
 for c in p['cells']:
  if c['kind']=='test-cycle' and any(c[s]['pin']==label for s in ['A','B']):
   name=c['selector'].strip('^$');assert name in names,(label,name)
print('PASS: 316 cells; frozen pins; baseline/target safety; 128 IDs; pinned test symbols')
