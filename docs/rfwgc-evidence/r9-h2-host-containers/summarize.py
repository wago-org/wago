#!/usr/bin/env python3
"""Read-only verdict construction with exact final-pin and nonzero census guards."""
import pathlib,json,hashlib
root=pathlib.Path(__file__).resolve().parent
pin='4bf395915fa2532b99bfec8f3a8d565cb6e4b217'
new=['TestR9H2HostContainer'+s for s in ['PublicGlobal','PublicTable','Close','Authority','Migration','Cycle']]
modes=['ordinary','wagodebug','wago_regalloccheck'];summary={'pin':pin,'required_A_tests':new,'lanes':{},'scope':'Slice A only; no combined acceptance, full-suite race or performance qualification'}
def get(p):return json.loads(p.read_text())
def passes(p):return [l.split()[2] for l in p.read_text().splitlines() if l.startswith('--- PASS:')]
for lane,count in [('integrated',18),('targets',14),('tinygo',3),('scoped',72)]:
 directory=root/(lane+'-4bf395915');rows=get(directory/'results.json')
 assert len(rows)==count,(lane,len(rows))
 assert all(r['exit']==0 for r in rows),lane
 if lane!='scoped':
  final=get(directory/'driver-final.json');assert final['driver_unchanged'],lane
  if lane!='targets':assert final['all_pass'],lane
  if lane=='integrated':assert not final['export_changes'],lane
  provenance=get(directory/('source.json' if lane=='targets' else 'provenance.json'));assert provenance['pin']==pin
  driver=pathlib.Path('docs/rfwgc-evidence/r9-h2-production')/('qualify-'+lane+'.py')
  assert provenance['driver_sha256']==hashlib.sha256(driver.read_bytes()).hexdigest(),lane
  summary['lanes'][lane]={'commands':count,'driver_sha256':provenance['driver_sha256'],'archive_sha256':provenance['archive_sha256'],'rows':[]}
 else:
  assert get(directory/'final.json')['all_pass']
  summary['lanes'][lane]={'commands':count,'rows':[]}
 for row in rows:
  label=row.get('label',row.get('mode'));p=directory/(label+'.log')
  names=passes(p);skip=row.get('skip',row.get('skips',[]))
  if lane=='integrated':
   expected={'focused':48,'race':48,'full':2497 if label.startswith('wagodebug-') else 2503,'backend':996 if label.startswith('wagodebug-') else 1121 if label.startswith('wago_regalloccheck-') else 998}
   key=label.split('-',1)[1]
   if key in expected:assert len(names)==expected[key],(label,len(names),expected[key])
   if key in ['focused','race','full']:assert set(new)<=set(names),label
   if key in ['focused','race']:assert not skip,label
  if lane=='targets' and label.endswith('-guest'):
   assert len(names)==(40 if label.startswith('arm64-') else 45),(label,len(names))
   assert not skip,label
   if label.startswith('arm64-'):assert set(new)<=set(names),label
   else:assert not (set(new)&set(names)),label
   elf=get(directory/(label+'-elf.json'));assert elf['machine']==(183 if label.startswith('arm64-') else 243)
   assert 'TestPlatformExecutionArchitecture' in names,label
  if lane=='tinygo':assert len(names)==5 and not skip,(label,names,skip)
  if lane=='scoped':assert sorted(names)==sorted(row['expected']) and names and not skip,label
  summary['lanes'][lane]['rows'].append({'label':label,'exit':row['exit'],'top_level_passes':len(names),'skip_entries':len(skip),'skip_names':skip})
(root/'qualification.json').write_text(json.dumps(summary,indent=2)+'\n')
print('PASS: exact 18 integrated, 14 target, 3 TinyGo, 72 scoped commands; source and test census verified')
