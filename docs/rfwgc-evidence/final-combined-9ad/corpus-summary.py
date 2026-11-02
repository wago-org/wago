#!/usr/bin/env python3
"""Every expected ID/revision/mode, with missing events explicitly NOT_RUN."""
import pathlib,json,gzip,collections
E=pathlib.Path(__file__).resolve().parent;S=pathlib.Path(json.loads((E/'source.json').read_text())['source'])
catalog_path=S/'corpus/catalog.json';catalog_path=catalog_path if catalog_path.exists() else E/'catalog.json'
cat=json.loads(catalog_path.read_text());(E/'catalog.json').write_text(json.dumps(cat,indent=2)+'\n')
rows=[]
for label,pin in [('port','9ad40bce4a7397dbadf0b0528f2587ed59d7d441'),('baseline','a3c08365f531eb3c9003e3acf3e0a822c12c31f4')]:
 for mode in ['ordinary','wagodebug','wago_regalloccheck']:
  name=('' if label=='port' else 'baseline-')+'corpus-'+mode;p=E/name;events={};output=collections.defaultdict(list)
  raw=p/'raw.log.gz';data=gzip.open(raw,'rt').read() if raw.exists() else (p/'raw.log').read_text() if (p/'raw.log').exists() else ''
  for line in data.splitlines():
   try:x=json.loads(line)
   except ValueError:continue
   t=x.get('Test')
   if t and x['Action'] in ('pass','fail','skip'):events[t]=x['Action']
   if t and x['Action']=='output':output[t].append(x.get('Output',''))
  for e in cat['benchmarks']:
   id=e['id'];stages=e.get('stages') or ['Decode','Validate','Compile','CompileFull','Instantiate'];checks={f'TestCorpus/{id}/{s}':events.get(f'TestCorpus/{id}/{s}','NOT_RUN') for s in stages if s not in ['Exec','CommandExec']}
   execution=[f"TestCorpus/{id}/Exec/{v['export']}" for v in e.get('exec',[])]+['TestCorpusSemanticExec/'+s for s in e.get('semantic_exec',[])]+([f'TestApplicationCorpusRuns/{id}/wago'] if e.get('command') else [])
   checks.update({s:events.get(s,'NOT_RUN') for s in execution});ref=None
   if e.get('command'):
    t=f'TestApplicationCorpusRuns/{id}/wazero';ref={'status':events.get(t,'NOT_RUN'),'pinned_external_runtime':e['command'].get('reference_runtime'),'detail':''.join(output[t]) if events.get(t)!='pass' else None}
   rows.append({'id':id,'revision':pin,'label':label,'mode':mode,'checks':checks,'reference':ref,'status':'PASS' if checks and all(v=='pass' for v in checks.values()) else 'FAIL' if 'fail'in checks.values() else 'INCOMPLETE','suite_completed':(p/'result.json').exists()})
(E/'corpus-per-id.json').write_text(json.dumps(rows,indent=2)+'\n');by={(r['id'],r['label'],r['mode']):r for r in rows};joins=[]
for e in cat['benchmarks']:
 for mode in ['ordinary','wagodebug','wago_regalloccheck']:
  b=by[e['id'],'baseline',mode];p=by[e['id'],'port',mode];joins.append({'id':e['id'],'mode':mode,'baseline':b['status'],'port':p['status'],'both_suites_completed':b['suite_completed'] and p['suite_completed'],'matched_pass':b['status']==p['status']=='PASS'})
(E/'corpus-joins.json').write_text(json.dumps(joins,indent=2)+'\n')
print('rows',len(rows),'joins',len(joins),'status',dict(collections.Counter(r['status'] for r in rows)))
