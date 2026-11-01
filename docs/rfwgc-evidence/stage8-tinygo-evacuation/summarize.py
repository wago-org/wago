#!/usr/bin/env python3
"""Summarize correctness receipts, preserving every named skip."""
from pathlib import Path
import json,re
root=Path(__file__).resolve().parent
rows=json.loads((root/'expanded/results.json').read_text())
assert len(rows)==11, 'expanded matrix incomplete'
summary=[]
for row in rows:
 text=(root/'expanded'/(row['label']+'.log')).read_text()
 item={'label':row['label'],'exit':row['exit']}
 for kind in ['PASS','FAIL','SKIP']:
  names=re.findall(r'^\s*--- '+kind+r': (\S+)',text,re.M)
  item[kind.lower()]=names
 if row['label'].startswith('tinygo-'):
  for name in ['TestEvacuationTinyGoRequiredPressure','TestEvacuationTinyGoCardsLOSAndYoung','TestEvacuationTinyGoCheckedGeneration','TestEvacuationTinyGoPolicy','TestEvacuationStage7LargeYoungPlan']:
   assert name in item['pass'], (row['label'],name)
  assert not item['skip']
 if row['label']=='tinygo-wagodebug':
  for name in ['TestEvacuationStage7AdmissionFaults','TestEvacuationStage7CommittedMaintenance','TestEvacuationStage7SerialPartialCopyRollback']:
   assert name in item['pass'], name
 if row['label'].startswith('joint-'):
  for name in ['TestGCStage8TinyGoGuestRelocatedRead','TestGCStage8TinyGoControls','TestTinyGoForeignStackConcurrentGC','TestTinyGoForeignStackGCHostReentry','TestTinyGoForeignStackGCPreparedEntry','TestTinyGoForeignStackGCCloseThunkFailure']:
   assert name in item['pass'], (row['label'],name)
  assert not item['skip']
 assert row['exit']==0 and not item['fail'], item
 summary.append(item)
assert (root/'expanded/driver-unchanged.txt').read_text()=='PASS\n'
assert (root/'expanded/source-unchanged.txt').read_text()=='PASS\n'
(root/'summary.json').write_text(json.dumps(summary,indent=2)+'\n')
for row in summary: print(row['label'], 'exit='+str(row['exit']), 'passes='+str(len(row['pass'])), 'failed='+str(len(row['fail'])), 'skips='+str(len(row['skip'])))
