"""Rebuild terminal verdicts without executing tests; absent work never passes."""
import pathlib,json,gzip,hashlib,collections,runpy
E=pathlib.Path(__file__).resolve().parent;M=json.loads((E/'source.json').read_text());modes=['ordinary','wagodebug','wago_regalloccheck'];rows=[]
def raw(d):
 p=d/'raw.log.gz';return gzip.open(p,'rt',errors='replace').read() if p.exists() else (d/'raw.log').read_text(errors='replace') if (d/'raw.log').exists() else ''
for p in sorted(E.glob('*/command.json')):
 c=json.loads(p.read_text());d=p.parent;r=json.loads((d/'result.json').read_text()) if (d/'result.json').exists() else {};events=r.get('events',[]);outputs=collections.defaultdict(list)
 for l in raw(d).splitlines():
  try:v=json.loads(l)
  except ValueError:continue
  if isinstance(v,dict) and v.get('Action')=='output':outputs[(v.get('Package',''),v.get('Test',''))].append(v.get('Output',''))
 plain=raw(d).splitlines()
 rows.append(dict(plain_passes=[l for l in plain if not l.startswith('{') and '--- PASS:' in l],plain_skips=[dict(terminal=l,detail='\n'.join(plain[max(0,i-4):i+1])) for i,l in enumerate(plain) if not l.startswith('{') and '--- SKIP:' in l],plain_failures=[l for l in plain if not l.startswith('{') and '--- FAIL:' in l],suite=d.name,source=c.get('source'),verdict=r.get('verdict','INTERRUPTED'),exit=r.get('exit'),command=str(p.relative_to(E)),pass_count=sum(e['Action']=='pass' and 'Test' in e for e in events),top_level_passes=[dict(package=e.get('Package'),test=e['Test']) for e in events if e['Action']=='pass' and e.get('Test') and '/' not in e['Test']],failures=[e for e in events if e['Action']=='fail'],skips=[dict(event=e,detail=''.join(outputs[(e.get('Package',''),e.get('Test',''))])) for e in events if e['Action']=='skip']))
expected=[]
for m in modes:
 for prefix in ['catalog','parent-race-reduced','full-root-replay','cli','installer','nested','collector','race','guard','corpus','baseline-corpus','graphs-port','graphs-baseline','backend','diagnostics-default','diagnostics-established','tinygo-evacuation','tinygo-joint','tinygo-conversion','spec-v3-signals','guard-corpus','runtime-cli-build','profile-metadata','profile-runtime','shared-scalar','native-join','seeded-concurrency']:
  expected.append(prefix+'-'+m)
 for v in [1,2,3]:expected.append('spec-v'+str(v)+'-'+m)
 for suffix in ['',',wago_guardpage']:
  tag=(m if m!='ordinary' else '')+suffix;tag=tag.strip(',') or 'ordinary'
  for prefix in ['hardening','host-transition']:expected.append(prefix+'-'+tag)
 for prefix in ['build','vet']:expected.append('tooling-'+prefix+'-'+m)
for n in ['generate','docs','node-update-website-bench','node-update-website-startup','node-engine-state-oracle','node-fuzz-engine-state','script-build-release-assets.sh','script-release-qualification.sh','script-ci-download-go-modules.sh','script-ci-riscv64','actionlint','script-check-diagnostic-dce.sh','script-check-riscv64-diagnostics.sh','script-check-regalloc-code.sh','staticcheck-bootstrap','staticcheck','native-compare','portable-386']:expected.append('tooling-'+n)
for n in ['docs','generated-diff','format','build-release-assets.sh','release-qualification.sh','ci-download-go-modules.sh','actionlint','staticcheck']:expected.append('replay-'+n)
for n in ['FuzzStructCardDifferential','FuzzValidateRelativePathAndRustScannerDoNotPanic','FuzzNormalizeWABTJSONDoesNotPanic','FuzzSpecTrapMatchingDoesNotPanic','FuzzDecodeValidateByteBackedDifferentialGenerated']:expected.append('fuzz-'+n)
for lane in ['qemu-collector','qemu-guest','qemu-facade']:
 p=E/lane/'results.json';rs=json.loads(p.read_text()) if p.exists() else []
 for r in rs:rows.append(dict(suite=lane+'/'+r['label'],source=M['pin'],verdict='PASS' if r['exit']==0 else 'FAIL',**r))
 for arch in ['arm64','riscv64']:
  for m in modes:
   for suffix in ['','-build']:expected.append(lane+'/'+arch+'-'+m+'-'+lane.removeprefix('qemu-')+suffix)
names=[x+'-'+m for m in modes for x in ['host','standalone','guest-threads']]+['ci-checkers','ci-runtime','ci-cli','ci-cli-smoke','ci-release']+[x+'-'+str(i) for i in range(1,11) for x in ['ci-release-version','ci-release-fib']]
p=E/'tinygo-34/qualification-results.txt';rs={}
if p.exists():
 for l in p.read_text().splitlines():
  n,rc=l.split(' exit=');rs[n]=int(rc)
for n in names:
 p=E/'tinygo-34'/(n+'.log');ls=p.read_text(errors='replace').splitlines() if p.exists() else gzip.open(str(p)+'.gz','rt',errors='replace').read().splitlines() if pathlib.Path(str(p)+'.gz').exists() else []
 rows.append(dict(suite='tinygo-34/'+n,source=M['pin'],exit=rs.get(n),verdict=('PASS' if rs[n]==0 else 'FAIL') if n in rs else 'INTERRUPTED' if p.exists() else 'NOT_RUN',passes=[l for l in ls if not l.startswith('{') and '--- PASS:' in l],skips=[l for l in ls if not l.startswith('{') and '--- SKIP:' in l],failures=[l for l in ls if not l.startswith('{') and '--- FAIL:' in l]))
seen={r['suite'] for r in rows}
for lane,arch in [('qemu-h6','arm64'),('qemu-rv64-controls','riscv64')]:
 p=E/lane/'results.json';rs=json.loads(p.read_text()) if p.exists() else []
 for r in rs:rows.append(dict(suite=lane+'/'+r['label'],source=M['pin'],verdict='PASS' if r['exit']==0 else 'FAIL',**r))
 for m in modes:
  for suffix in ['','-build']:expected.append(lane+'/'+arch+'-'+m+'-guest'+suffix)
seen={r['suite'] for r in rows}
for n in expected:
 if n not in seen:rows.append(dict(suite=n,source=M['pin'],verdict='NOT_RUN',reason='Not executed; no terminal pass evidence.'))
rows.append(dict(suite='native-Darwin',source=M['pin'],verdict='NOT_RUN',reason='No established native or remote Darwin runner. Acquisition cost unknown.'))
(E/'qualification.json').write_text(json.dumps(dict(source=M['pin'],rows=rows,counts=dict(collections.Counter(r['verdict'] for r in rows))),indent=2)+'\n')
runpy.run_path(str(E/'corpus-summary.py'),run_name='__main__')
print('verdicts',dict(collections.Counter(r['verdict'] for r in rows)))
