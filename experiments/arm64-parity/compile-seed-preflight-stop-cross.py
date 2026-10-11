from pathlib import Path
import json, os, subprocess
root=Path('experiments/arm64-parity')
names='audio-adpcm,files-glob-match,geo-point-in-polygon,video-dct,graphics-reed-solomon,compiler-register-allocation'
rows=[]
for pair in range(4):
 for candidate in [bool(pair%2),not bool(pair%2)]:
  env=dict(os.environ,WAGO_SHARED_SCALAR='0')
  for feature in ['REGIONAL_MEMORY_READ','DOMINATED_INDEXED_KEY','BOUNDED_SIGNED_QUOTIENT','WIDE_REUSE_PINS','ORDINARY_SHIFTED_IMMEDIATE']:
   env.pop('WAGO_ARM64_NO_'+feature,None);env.pop('WAGO_ARM64_EXPERIMENT_'+feature,None)
  binary='/tmp/paired-compile-seed-preflight-stop' if candidate else '/tmp/paired-hour-20261011'
  state='candidate' if candidate else 'before'
  raw=subprocess.run([binary,'-option','loop-memory-base','-workloads',names,'-phase','compile','-rounds','1','-budget','100ms'],env=env,text=True,capture_output=True,check=True).stdout
  (root/f'compile-seed-preflight-stop-{pair}-{state}.txt').write_text(raw)
  for line in raw.splitlines():
   if line.startswith('{'):
    row=json.loads(line)
    if row['on']:
     row['on']=candidate;row['round']=pair;rows.append(row)
  (root/'compile-seed-preflight-stop-cross.jsonl').write_text(''.join(json.dumps(x)+'\n' for x in rows))
with (root/'compile-seed-preflight-stop-summary.md').open('w') as out:
 subprocess.run(['python3',str(root/'summarize_paired.py'),str(root/'compile-seed-preflight-stop-cross.jsonl')],stdout=out,check=True)
