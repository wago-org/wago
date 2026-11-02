#!/usr/bin/env python3
"""Rebuild exact terminal-event accounting without running tests."""
import pathlib,json,hashlib,gzip
E=pathlib.Path(__file__).resolve().parent
ledger_path=E/'gates/ledger.json'
ledger=json.loads(ledger_path.read_text() if ledger_path.exists() else gzip.decompress((E/'gates/ledger.json.gz').read_bytes()))
source=json.loads((E/'source.json').read_text())
catalog=json.loads((E/'catalog.json').read_text())
byname={x['name']:x for x in ledger}
rows=[]
for mode in ['ordinary','wagodebug','wago_regalloccheck']:
 command=byname.get('catalog-'+mode,{})
 events={x.get('Test'):x['Action'] for x in command.get('events',[]) if x.get('Test')}
 for entry in catalog['benchmarks']:
  rows.append(dict(mode=mode,id=entry['id'],artifact_sha256=entry['artifact_sha256'],terminal=events.get('TestRFWStage3CorpusCompile/'+entry['id'],'NOT_RUN'),command_exit=command.get('exit')))
(E/'catalog-per-id.json').write_text(json.dumps(rows,indent=2)+'\n')
summary=[]
for gate in ledger:
 events=gate['events'];counts={action:sum(x['Action']==action for x in events if x.get('Test')) for action in ['pass','fail','skip']}
 summary.append(dict(name=gate['name'],exit=gate['exit'],verdict='PASS' if gate['exit']==0 else 'FAIL',terminal_test_events=counts,failures=[x for x in events if x['Action']=='fail'],skips=[x for x in events if x['Action']=='skip']))
(E/'qualification.json').write_text(json.dumps(dict(source=source['pin'],gates=summary,catalog_rows=len(rows),catalog_pass=sum(x['terminal']=='pass' for x in rows)),indent=2)+'\n')
print(json.dumps(dict(gates=len(summary),failed=[g['name'] for g in summary if g['exit']],catalog_rows=len(rows),catalog_pass=sum(x['terminal']=='pass' for x in rows))))
execution=[]
for mode in ['ordinary','wagodebug','wago_regalloccheck']:
 gate=byname.get('corpus-execution-'+mode,{})
 events={x.get('Test'):x['Action'] for x in gate.get('events',[]) if x.get('Test')}
 for entry in catalog['benchmarks']:
  id=entry['id'];stages=entry.get('stages') or ['Decode','Validate','Compile','CompileFull','Instantiate']
  names=[f'TestCorpus/{id}/{s}' for s in stages if s not in ['Exec','CommandExec']]
  names += [f"TestCorpus/{id}/Exec/{v['export']}" for v in entry.get('exec',[])]
  names += ['TestCorpusSemanticExec/'+s for s in entry.get('semantic_exec',[])]
  if entry.get('command'):names.append(f'TestApplicationCorpusRuns/{id}/wago')
  checks={name:events.get(name,'NOT_RUN') for name in names}
  reference=None
  if entry.get('command'):
   reference=dict(terminal=events.get(f'TestApplicationCorpusRuns/{id}/wazero','NOT_RUN'),pinned_external_runtime=entry['command'].get('reference_runtime'))
  execution.append(dict(id=id,mode=mode,checks=checks,reference=reference,command_exit=gate.get('exit'),verdict='PASS' if gate.get('exit')==0 and checks and all(v=='pass' for v in checks.values()) else 'FAIL' if 'fail' in checks.values() else 'INCOMPLETE'))
(E/'corpus-per-id.json').write_text(json.dumps(execution,indent=2)+'\n')
print('Execution rows:',len(execution),'PASS:',sum(r['verdict']=='PASS' for r in execution))
