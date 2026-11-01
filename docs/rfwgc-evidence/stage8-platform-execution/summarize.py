#!/usr/bin/env python3
"""Summarize retained correctness results, keeping setup failures separate."""
import json,pathlib
root=pathlib.Path(__file__).resolve().parent
rows=[]
for directory,suite in [('qualified','collector'),('guest-replay','guest')]:
 for arch in ['arm64','riscv64']:
  chosen='rv64-guest-replay' if suite=='guest' and arch=='riscv64' else directory
  data=json.loads((root/chosen/'results.json').read_text())
  for mode in ['ordinary','wagodebug','wago_regalloccheck']:
   label=f'{arch}-{mode}-{suite}'
   matching=[r for r in data if r['label']==label]
   if len(matching)!=1:raise SystemExit('missing result '+label)
   r=matching[0];text=(root/chosen/(label+'.log')).read_text()
   required=['TestEvacuationStage7Moves','TestParallelStage6PortableOwnerCallbacks','TestParallelStage6PolicyOracle'] if suite=='collector' else ['TestPlatformExecutionArchitecture','TestGCStage7GuestWorkers','TestGCStage7GuestRelocatedRead','TestGCHostReentryFullDepth']
   for test in required:
    if not any(('--- PASS: '+test+' ') in line for line in r['pass']):raise SystemExit('missing mandatory execution '+label+':'+test)
   if suite=='guest' and 'EXECUTED linux/'+arch not in text:raise SystemExit('missing architecture probe')
   rows.append({'arch':arch,'mode':mode,'suite':suite,'exit':r['exit'],'passes':len(r['pass']),'skips':r['skip'],'fails':r['fail'],'directory':chosen})
(root/'summary.json').write_text(json.dumps(rows,indent=2)+'\n')
print('| Target | Mode | Collector pass / skip | Guest pass / skip |')
print('| --- | --- | ---: | ---: |')
for arch in ['arm64','riscv64']:
 for mode in ['ordinary','wagodebug','wago_regalloccheck']:
  cell=[next(r for r in rows if r['arch']==arch and r['mode']==mode and r['suite']==s) for s in ['collector','guest']]
  print('| '+arch+' | '+mode+' | '+' | '.join(f"{r['passes']} / {len(r['skips'])}" for r in cell)+' |')
raise SystemExit(any(r['exit'] or r['fails'] for r in rows))
