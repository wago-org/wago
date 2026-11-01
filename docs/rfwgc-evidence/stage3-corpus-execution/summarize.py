#!/usr/bin/env python3
"""Join every catalog row to exact terminal test events without using durations."""
import collections,json,pathlib
HERE=pathlib.Path(__file__).resolve().parent
PINS=json.loads((HERE/'identity.json').read_text())['pins']
cat=json.loads((HERE/'catalog.json').read_text())
rows=[];runs=[]
for rec in json.loads((HERE/'results.json').read_text()):
 events={};output=collections.defaultdict(list)
 for line in (HERE/rec['raw']).read_text().splitlines():
  try:e=json.loads(line)
  except ValueError:continue
  t=e.get('Test')
  if t and e['Action'] in ('pass','fail','skip'):events[t]=e['Action']
  if t and e['Action']=='output':output[t].append(e['Output'])
 runs.append(rec|{'terminal_counts':dict(collections.Counter(events.values())),'failures':[t for t,s in events.items() if s=='fail'],'skips':{t:''.join(output[t]) for t,s in events.items() if s=='skip'}})
 for entry in cat['benchmarks']:
  id=entry['id'];stage=entry.get('stages') or ['Decode','Validate','Compile','CompileFull','Instantiate']
  checks={f'TestCorpus/{id}/{s}':events.get(f'TestCorpus/{id}/{s}','NOT_RUN') for s in stage if s not in ['Exec','CommandExec']}
  execution=[]
  for v in entry.get('exec',[]):execution.append(f"TestCorpus/{id}/Exec/{v['export']}")
  for s in entry.get('semantic_exec',[]):execution.append('TestCorpusSemanticExec/'+s)
  if entry.get('command'):execution.append(f'TestApplicationCorpusRuns/{id}/wago')
  checks.update({s:events.get(s,'NOT_RUN') for s in execution})
  reference=None
  if entry.get('command'):
   t=f'TestApplicationCorpusRuns/{id}/wazero'
   reference={'test':t,'status':events.get(t,'NOT_RUN'),'pinned_external_runtime':entry['command'].get('reference_runtime'),'detail':''.join(output[t]) if events.get(t)!='pass' else None}
  status='pass' if checks and all(s=='pass' for s in checks.values()) else 'fail' if 'fail' in checks.values() else 'incomplete'
  rows.append({'id':id,'pin':rec['pin'],'revision':rec['revision'],'mode':rec['mode'],'policy':'default/off','artifact_sha256':entry['artifact_sha256'],'status':status,'checks':checks,'reference':reference})
by={(r['id'],r['pin'],r['mode']):r for r in rows}
joins=[]
for e in cat['benchmarks']:
 for mode in ['ordinary','wagodebug','wago_regalloccheck']:
  b=by.get((e['id'],'baseline',mode));p=by.get((e['id'],'port',mode))
  if not b or not p:continue
  if b['checks']==p['checks']:
   disposition='baseline-matched-pass' if b['status']=='pass' else 'baseline-matched-failure' if b['status']=='fail' else 'baseline-matched-incomplete'
  else:disposition='regression' if b['status']=='pass' and p['status']!='pass' else 'improvement' if p['status']=='pass' else 'different-failure-or-coverage'
  joins.append({'id':e['id'],'mode':mode,'baseline':b['status'],'port':p['status'],'disposition':disposition,'reference_match':(b['reference'] or {}).get('status')==(p['reference'] or {}).get('status'),'baseline_reference':(b['reference'] or {}).get('status'),'port_reference':(p['reference'] or {}).get('status')})
summary={'denominator':128,'unique_binaries':len({e['artifact_sha256'] for e in cat['benchmarks']}),'input_references':sum(len(e.get('command',{}).get('inputs',{})) for e in cat['benchmarks']),'rows':len(rows),'joins':len(joins),'dispositions':dict(collections.Counter(j['disposition'] for j in joins)),'runs':runs}
for name,data in [('per-id-revision-mode.json',rows),('revision-joins.json',joins),('summary.json',summary)]: (HERE/name).write_text(json.dumps(data,indent=2)+'\n')
print(json.dumps({k:v for k,v in summary.items() if k!='runs'},indent=2))
