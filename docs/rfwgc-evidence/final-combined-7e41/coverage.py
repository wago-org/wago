"""Require nonzero exact selected coverage in addition to command success."""
import pathlib,json,re
E=pathlib.Path(__file__).resolve().parent;rows=[]
p=E/'qemu-guest/results.json'
for r in json.loads(p.read_text()) if p.exists() else []:
 if not r['label'].endswith('-guest'):continue
 top=[re.match(r'^--- PASS: ([^ /]+)',l).group(1) for l in r['pass'] if re.match(r'^--- PASS: ([^ /]+)',l)]
 a=[n for n in top if n.startswith('TestR9H2HostContainer')];b=[n for n in top if n.startswith('TestR9H2HostImport')];h6=[n for n in top if n.startswith('TestR9Ownership')]
 if r['label'].startswith('arm64'):
  hr=next(x for x in json.loads((E/'qemu-h6/results.json').read_text()) if x['label']==r['label'])
  h6=[re.match(r'^--- PASS: ([^ /]+)',l).group(1) for l in hr['pass'] if re.match(r'^--- PASS: TestR9Ownership[^ /]+',l)]
  assert hr['exit']==0 and not hr['skip']
 row=dict(suite=r['label'],top_level_passes=top,A=a,B=b,H6=h6,skips=r['skip'],architecture_probe='TestPlatformExecutionArchitecture' in top);rows.append(row)
 assert row['architecture_probe'],r['label']
 if r['label'].startswith('arm64'):
  assert len(a)==6 and len(b)==9 and len(h6)==9,(r['label'],a,b,h6)
 else:
  assert not a and not b,'Opaque host tests must not be claimed on RV64'
(E/'target-coverage.json').write_text(json.dumps(rows,indent=2)+'\n')
for n in ['native-join','seeded-concurrency','profile-runtime','profile-metadata','parent-race-reduced']:
 for m in ['ordinary','wagodebug','wago_regalloccheck']:
  p=E/(n+'-'+m)/'result.json'
  if not p.exists():continue
  r=json.loads(p.read_text());ts=[e for e in r['events'] if e.get('Test') and e['Action']=='pass'];assert r['exit']==0 and ts,(n,m)
print('target coverage receipts',len(rows),'nonzero host supplement checks passed')

# Full existing RV64 managed-function family, not merely its Joint subset.
src=pathlib.Path(json.loads((E/'source.json').read_text())['source']);expected=set()
for f in (src/'src/wago').glob('function_environment*_riscv64_test.go'):
 expected.update(re.findall(r'^func (TestRV64ManagedFunction\w+|TestManagedFunctionRV64Shape)\(',f.read_text(),re.M))
p=E/'qemu-rv64-controls/results.json';rv=[]
if p.exists():
 for r in json.loads(p.read_text()):
  if not r['label'].endswith('-guest'):continue
  top={re.match(r'^--- PASS: ([^ /]+)',l).group(1) for l in r['pass'] if re.match(r'^--- PASS: ([^ /]+)',l)}
  assert expected<=top,(r['label'],sorted(expected-top))
  rv.append(dict(suite=r['label'],expected=sorted(expected),passed=sorted(top),skips=r['skip']))
(E/'rv64-managed-function-coverage.json').write_text(json.dumps(rv,indent=2)+'\n')
# TinyGo's non-JSON test runner must show actual terminal test execution.
import gzip
tg=[]
for mode in ['ordinary','wagodebug','wago_regalloccheck']:
 for label,want in [('evacuation',30 if mode=='wagodebug' else 15),('joint',9),('conversion',5)]:
  d=E/('tinygo-'+label+'-'+mode)
  if not (d/'result.json').exists():continue
  r=json.loads((d/'result.json').read_text());lines=gzip.open(d/'raw.log.gz','rt').read().splitlines();passes=[l for l in lines if '--- PASS:' in l];skips=[l for l in lines if '--- SKIP:' in l]
  assert r['exit']==0 and len(passes)==want and not skips,(d.name,r['exit'],len(passes),skips)
  tg.append(dict(suite=d.name,passes=passes,skips=skips,expected_pass_events=want))
(E/'tinygo-supplement-coverage.json').write_text(json.dumps(tg,indent=2)+'\n')
