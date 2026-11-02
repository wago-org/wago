"""Require nonzero exact selected coverage in addition to command success."""
import pathlib,json,re
E=pathlib.Path(__file__).resolve().parent;rows=[]
p=E/'qemu-guest/results.json'
for r in json.loads(p.read_text()) if p.exists() else []:
 if not r['label'].endswith('-guest'):continue
 top=[re.match(r'^--- PASS: ([^ /]+)',l).group(1) for l in r['pass'] if re.match(r'^--- PASS: ([^ /]+)',l)]
 a=[n for n in top if n.startswith('TestR9H2HostContainer')];b=[n for n in top if n.startswith('TestR9H2HostImport')];h6=[n for n in top if n.startswith('TestR9Ownership')]
 row=dict(suite=r['label'],top_level_passes=top,A=a,B=b,H6=h6,skips=r['skip'],architecture_probe='TestPlatformExecutionArchitecture' in top);rows.append(row)
 assert row['architecture_probe'],r['label']
 if r['label'].startswith('arm64'):
  assert len(a)==6 and len(b)==9 and h6,(r['label'],a,b,h6)
 else:
  assert not a and not b,'Opaque host tests must not be claimed on RV64'
(E/'target-coverage.json').write_text(json.dumps(rows,indent=2)+'\n')
for n in ['native-join','seeded-concurrency','profile-runtime','profile-metadata','parent-race-reduced']:
 for m in ['ordinary','wagodebug','wago_regalloccheck']:
  p=E/(n+'-'+m)/'result.json'
  if not p.exists():continue
  r=json.loads(p.read_text());ts=[e for e in r['events'] if e.get('Test') and e['Action']=='pass'];assert r['exit']==0 and ts,(n,m)
print('target coverage receipts',len(rows),'nonzero host supplement checks passed')
