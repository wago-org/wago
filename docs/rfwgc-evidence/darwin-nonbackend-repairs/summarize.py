#!/usr/bin/env python3
import hashlib,json,pathlib,re
out=pathlib.Path(__file__).resolve().parent
q=json.loads((out/'qualification.json').read_text())
r=json.loads((out/'foreign-replay.json').read_text())
assert q['source']==r['source']
assert len(q['cells'])==9 and len(r['cells'])==3
rows=[]
for c in q['cells']:
 assert c['build_exit']==0
 row={'mode':c['mode'],'target':c['target'],'build':'PASS'}
 if c['target']=='darwin-arm64': row['execution']='NOT_RUN'
 else:
  log=(out/(c['target']+'-'+c['mode']+'-test.log')).read_text()
  results=dict((name,status) for status,name in re.findall(r'^--- (PASS|FAIL|SKIP): (\S+)',log,re.M))
  assert len(results)==(11 if c['target']=='linux-arm64' else 2)
  assert 'SKIP' not in results.values()
  row['original_results']=results.copy()
  if c['target']=='linux-arm64':
   replay=next(x for x in r['cells'] if x['mode']==c['mode'])
   assert replay['execution_exit']==0 and replay['binary_sha256']==c['binary_sha256']
   replaylog=(out/('linux-arm64-'+c['mode']+'-foreign-replay.log')).read_text()
   assert '--- PASS: TestSharedEntryGuardForeignFallback ' in replaylog
   results['TestSharedEntryGuardForeignFallback']='PASS (same-binary WABT replay)'
   assert results['TestSharedEntryGuardCancellationAndGC']=='FAIL'
  else: assert c['execution_exit']==0
  row['results_after_harness_replay']=results
  row['pass_count']=sum(s.startswith('PASS') for s in results.values())
  row['fail_count']=sum(s=='FAIL' for s in results.values())
 rows.append(row)
(out/'verdicts.json').write_text(json.dumps({'source':q['source'],'cells':rows},indent=2)+'\n')
print('\n'.join(str({k:v for k,v in r.items() if k not in ['original_results','results_after_harness_replay']}) for r in rows))
