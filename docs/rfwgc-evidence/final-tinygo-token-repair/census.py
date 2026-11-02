import pathlib,json,re,hashlib,gzip
E=pathlib.Path(__file__).resolve().parent; M=json.loads((E/'source.json').read_text()); S=pathlib.Path(M['workspace'])/'tinygo-source'
def sha(p):return hashlib.sha256(p.read_bytes()).hexdigest()
def rawbytes(p):
 return p.read_bytes() if p.exists() else gzip.decompress(pathlib.Path(str(p)+".gz").read_bytes())
expected=[]
for mode in ['ordinary','wagodebug','wago_regalloccheck']:
 expected += [x+'-'+mode for x in ['host','standalone','guest-threads']]
expected+=['ci-checkers','ci-runtime','ci-cli','ci-cli-smoke','ci-release']
for i in range(1,11):expected += [f'ci-release-version-{i}',f'ci-release-fib-{i}']
results={}
for line in (E/'tinygo-34/qualification-results.txt').read_text().splitlines():
 name,rc=line.split(' exit='); assert name not in results;results[name]=int(rc)
rows=[]
for name in expected:
 log=E/'tinygo-34'/(name+'.log');text=rawbytes(log).decode() if log.exists() or pathlib.Path(str(log)+'.gz').exists() else ''
 events={kind:re.findall(r'^\s*--- '+kind+r': (\S+)',text,re.M) for kind in ['PASS','FAIL','SKIP']}
 runs=re.findall(r'^\s*=== RUN\s+(\S+)',text,re.M)
 row=dict(gate=name,exit=results.get(name),events=events,run=runs,sha256=hashlib.sha256(rawbytes(log)).hexdigest() if log.exists() or pathlib.Path(str(log)+'.gz').exists() else None)
 if name in results and results[name]==0:
  assert not events['FAIL'],name
  if name.startswith(('host-','guest-threads-')) or name=='ci-runtime':
   for test in ['TestNullableLocalExternrefGlobals','TestLocalExternrefGlobalsRespectFeatureStoreAndLifetimeBoundaries','TestLocalExternrefGlobalsRespectFeatureStoreAndLifetimeBoundaries/cross-runtime','TestLocalExternrefGlobalsRespectFeatureStoreAndLifetimeBoundaries/forged']:
    assert test in events['PASS'],(name,test)
  if name.startswith('guest-threads-'):
   old=E.parent/'final-combined-f2bb/tinygo-34/guest-threads-ordinary.log'
   historical=set(re.findall(r'^\s*=== RUN\s+(\S+)',old.read_text(),re.M))
   # Modes may add diagnostic tests; every previously executed test must still run.
   row['historical_missing']=sorted(historical-set(runs)); assert not row['historical_missing'],row['historical_missing']
 rows.append(row)
(E/'full34-census.json').write_text(json.dumps(dict(pin=M['pin'],expected_count=34,passed=sum(x==0 for x in results.values()),unrun=[x for x in expected if x not in results],gates=rows),indent=2))
before=json.loads((E/'full34-export-before.json').read_text()); changed=[p for p,h in before.items() if not (S/p).is_file() or sha(S/p)!=h]
(E/'full34-integrity.json').write_text(json.dumps(dict(changed=changed,driver_unchanged=sha(E/'tinygo-34.sh')==M['driver_sha256']),indent=2));assert not changed
if (E/'supplement-results.json').exists():
 sup=json.loads((E/'supplement-results.json').read_text());
 for row in sup:
  if row['exit']==0 and not row['label'].endswith('-build'):assert row['passed'],row['label']
 (E/'attempt1-gate-summary.json').write_text(json.dumps(dict(pin=M['pin'],full34_pass=sum(x==0 for x in results.values()),supplement_pass=sum(r['exit']==0 for r in sup),supplement_execution_pass=sum(r['exit']==0 and not r['label'].endswith('-build') for r in sup),failed=[r['label'] for r in sup if r['exit']],historical_passes_not_reused=173),indent=2))
print('full34',len(results),'completed;',sum(x==0 for x in results.values()),'passed')
