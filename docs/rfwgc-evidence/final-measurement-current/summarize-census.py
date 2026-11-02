#!/usr/bin/env python3
"""Extract actual diagnostic census, never derive timing or peak-memory claims."""
import pathlib,json,re
H=pathlib.Path(__file__).resolve().parent;rows=[]
for family in ['qualification','detailed-census']:
 for receipt in json.loads((H/family/'receipts.json').read_text()):
  log=H/receipt['log'];subtest=None
  for line in log.read_text().splitlines():
   if line.startswith('=== RUN'):subtest=line.split()[-1]
   if 'FM_CENSUS ' not in line:continue
   census=json.loads(line.split('FM_CENSUS ',1)[1]);activity=census.get('ParallelActivity',{})
   # Array index n-1 counts phases joined by n participants, NOT per-worker work.
   actual={phase:[i+1 for i,n in enumerate(counts) if n] for phase,counts in activity.items()}
   row={'receipt':receipt['log'],'test':subtest,'requested_workers':receipt['env'].get('FM_WORKERS','1'),'region_shift':receipt['env'].get('FM_SHIFT','0'),'object_edges':receipt['env'].get('FM_OBJECT_EDGES','0'),'observed_joined_phase_participant_counts':actual,'census':census}
   rows.append(row)
(H/'participant-census.json').write_text(json.dumps({'interpretation':'ParallelActivity arrays are phase histograms by joined participant count. They do not prove equal work per participant. Endpoints are not peak RSS; live_payload_bytes_by_space includes headers. Serial zero counters are not missing workers. Requested workers are not actual participation.','rows':rows},indent=2)+'\n')
print('diagnostic snapshots',len(rows))
