#!/usr/bin/env python3
import pathlib,json,gzip,collections
E=pathlib.Path(__file__).resolve().parent
rows=[]
for f in sorted(E.glob('*/result.json')):
 d=json.loads(f.read_text());events=d['events'];skips={};failures={};outputs=collections.defaultdict(list)
 log=f.parent/'raw.log.gz'
 if log.exists():
  with gzip.open(log,'rt',errors='replace') as stream:
   for line in stream:
    try:x=json.loads(line)
    except ValueError:continue
    if x.get('Action')=='output':outputs[(x.get('Package'),x.get('Test'))].append(x.get('Output',''))
 for x in events:
  if 'Test' not in x:continue
  key=(x.get('Package'),x['Test'])
  if x['Action']=='skip':skips[':'.join(key)]=''.join(outputs[key])
  if x['Action']=='fail':failures[':'.join(key)]=''.join(outputs[key])
 rows.append({'suite':f.parent.name,'source':d['source'],'exit':d['exit'],'verdict':d['verdict'],'top_level_passes':sum(x['Action']=='pass' and 'Test' in x and '/' not in x['Test'] for x in events),'all_pass_events':sum(x['Action']=='pass' and 'Test' in x for x in events),'skips':skips,'failures':failures,'log':str(log.relative_to(E))})
for f in sorted(E.glob('qemu-*/results.json')):
 for r in json.loads(f.read_text()):rows.append({'suite':f.parent.name+'/'+r['label'],'source':json.loads((f.parent/'source.json').read_text())['pin'],'exit':r['exit'],'verdict':'PASS' if r['exit']==0 else 'FAIL','all_pass_events':len(r['pass']),'skips':r['skip'],'failures':r['fail'],'log':str((f.parent/(r['label']+'.log')).relative_to(E))})
(E/'summary.json').write_text(json.dumps(rows,indent=2)+'\n')
print(json.dumps({'commands':len(rows),'verdicts':dict(collections.Counter(r['verdict'] for r in rows)),'failed_suites':[r['suite'] for r in rows if r['verdict']=='FAIL']},indent=2))
