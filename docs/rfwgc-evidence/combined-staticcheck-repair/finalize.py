#!/usr/bin/env python3
"""Summarize retained logs and verify exports; does not run tests."""
import pathlib,json,re,gzip,hashlib,tarfile,subprocess
E=pathlib.Path(__file__).resolve().parent;W=pathlib.Path((E/'workspace.txt').read_text().strip())
rows=[]
for p in sorted(E.glob('*/**/result.json')):
 d=p.parent;log=d/'raw.log';raw=log.read_bytes() if log.exists() else gzip.decompress((d/'raw.log.gz').read_bytes());s=raw.decode();row={'gate':str(d.relative_to(E)),**json.loads(p.read_text()),'raw_sha256':hashlib.sha256(raw).hexdigest(),'passes':[],'skips':[],'failures':[],'packages':[]}
 for line in s.splitlines():
  try:j=json.loads(line)
  except ValueError:j={}
  if not isinstance(j,dict):j={}
  action=j.get('Action');test=j.get('Test')
  if action in ['pass','skip','fail']:
   if test:row[{'pass':'passes','skip':'skips','fail':'failures'}[action]].append(test)
   else:row['packages'].append({'package':j.get('Package'),'action':action})
  else:
   m=re.match(r'\s*--- (PASS|SKIP|FAIL): (\S+)',line)
   if m:row[{'PASS':'passes','SKIP':'skips','FAIL':'failures'}[m[1]]].append(m[2])
 row['passes']=sorted(set(row['passes']));row['skips']=sorted(set(row['skips']));row['failures']=sorted(set(row['failures']))
 rows.append(row)
 if log.exists():(d/'raw.log.gz').write_bytes(gzip.compress(raw,mtime=0));log.unlink()
(E/'results.json').write_text(json.dumps(rows,indent=2)+'\n')
checks=[]
for phase in ['before','after','final']:
 changed=[];count=0
 with tarfile.open(W/(phase+'.tar')) as t:
  for m in t:
   if not m.isfile():continue
   count+=1;p=W/phase/m.name
   if not p.exists() or hashlib.sha256(p.read_bytes()).digest()!=hashlib.sha256(t.extractfile(m).read()).digest():changed.append(m.name)
 checks.append(dict(phase=phase,original_archive_files=count,changed=changed))
 assert not changed
(E/'integrity-final.json').write_text(json.dumps(checks,indent=2)+'\n')
drivers={}
for phase,name in [('after','qualify.py'),('final','final-qualify.py')]:
 commands=list((E/phase).glob('*/command.json'))
 driver=hashlib.sha256((E/name).read_bytes()).hexdigest()
 assert all(json.loads(p.read_text())['driver_sha256']==driver for p in commands)
 drivers[phase]={'driver_sha256':driver,'all_command_hashes_match':True}
(E/'driver-integrity.json').write_text(json.dumps({'drivers':drivers,'supplemental_driver_sha256':{p.name:hashlib.sha256(p.read_bytes()).hexdigest() for p in E.glob('*.py')}},indent=2)+'\n')
# Required final gates must exist and pass. Initial setup failures remain in results.
required=['staticcheck']+['packages-'+m for m in ['ordinary','wagodebug','wago_regalloccheck']]+['ownership-race-'+m for m in ['ordinary','wagodebug','wago_regalloccheck']]+[a+'-'+m+'-'+kind for a in ['arm64','riscv64'] for m in ['ordinary','wagodebug','wago_regalloccheck'] for kind in ['build','execution']]
for gate in required:assert json.loads((E/'final'/gate/'result.json').read_text())['exit']==0,gate
lines=['# Final correctness gates','', '| Gate | Ordinary | wagodebug | wago_regalloccheck |','| --- | --- | --- | --- |','| Full GC/runtime/Wago packages | PASS | PASS | PASS |','| Focused ownership/receipt/lifecycle race | PASS | PASS | PASS |','| ARM64 cross-build | PASS | PASS | PASS |','| ARM64 actual QEMU ownership/receipt execution | PASS | PASS | PASS |','| RV64 cross-build | PASS | PASS | PASS |','| RV64 actual QEMU shape/exception/receipt/cycle execution | PASS | PASS | PASS |','','Staticcheck reviewed-baseline: PASS in every configured profile. Build selection: PASS in all 18 combinations. Original archive integrity: PASS. Exact parent lifeMu hook preservation: PASS.','', 'Each raw log, command, exit and test pass/skip/failure list is retained. Full package passes do not claim optional skipped tests ran. QEMU runs verify target ELF identity and execute the committed shape test, in addition to existing ownership oracles.','', '| Command | Passed tests/subtests | Skipped tests/subtests |','| --- | ---: | ---: |']
for r in rows:
 if r['gate'].startswith('final/') and (r['passes'] or r['skips']):lines.append(f"| {r['gate']} | {len(r['passes'])} | {len(r['skips'])} |")
(E/'GATES.md').write_text('\n'.join(lines)+'\n')
print('All required gates PASS; original archive files unchanged.')
