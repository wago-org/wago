"""Reversed-process default-on comparison with the pristine hour-start compiler.

Each raw process also records loop-memory-base off, but the declared cross-build
comparison uses its on state (the ordinary retained policy) in both binaries.
"""
from pathlib import Path
import json,os,subprocess

root=Path('experiments/arm64-parity')
names='audio-adpcm,video-dct,graphics-reed-solomon,files-glob-match,image-median,compiler-register-allocation,vision-components,geo-point-in-polygon'
features=['REGIONAL_MEMORY_READ','DOMINATED_INDEXED_KEY','BOUNDED_SIGNED_QUOTIENT','WIDE_REUSE_PINS','ORDINARY_SHIFTED_IMMEDIATE']
rows=[]
for phase in ['exec','compile']:
 for pair in range(4):
  for candidate in [bool(pair%2),not bool(pair%2)]:
   env=dict(os.environ,WAGO_SHARED_SCALAR='0')
   for feature in features:
    env.pop('WAGO_ARM64_NO_'+feature,None)
    env['WAGO_ARM64_EXPERIMENT_'+feature]=str(int(candidate))
   binary='/tmp/paired-hour-20261011' if candidate else '/tmp/paired-hour-base-c74d'
   state='candidate' if candidate else 'baseline'
   raw=subprocess.run([binary,'-option','loop-memory-base','-workloads',names,'-phase',phase,'-rounds','1','-budget','100ms','-code-dir','/tmp/hour-20261011-cross-'+state],env=env,text=True,capture_output=True,check=True).stdout
   (root/f'hour-20261011-cross-{phase}-{pair}-{state}.txt').write_text(raw)
   for line in raw.splitlines():
    if not line.startswith('{'):
     continue
    row=json.loads(line)
    if not row['on']:
     continue
    row['on']=candidate
    row['round']=pair
    rows.append(row)
   (root/'hour-20261011-cross-build.jsonl').write_text(''.join(json.dumps(x)+'\n' for x in rows))
proof=[]
for name in names.split(','):
 candidate=Path('/tmp/hour-20261011-cross-candidate')/(name+'-1.bin')
 baseline=Path('/tmp/hour-20261011-cross-baseline')/(name+'-1.bin')
 assert candidate.read_bytes()==(Path('/tmp/hour-20261011-app')/(name+'.bin')).read_bytes(),name
 assert baseline.read_bytes()==(Path('/tmp/branch-vector-retained-app')/(name+'.bin')).read_bytes(),name
 proof.append(name+': candidate and pristine baseline images match qualified inventories.')
(root/'hour-20261011-cross-native-proof.txt').write_text('\n'.join(proof)+'\n')
with (root/'hour-20261011-cross-build-summary.md').open('w') as out:
 subprocess.run(['python3',str(root/'summarize_paired.py'),str(root/'hour-20261011-cross-build.jsonl')],stdout=out,check=True)

p=root/'hour-20261011-cross-build-summary.md'
p.write_text(p.read_text().replace('Paired ratios retain the alternating same-thread comparisons;', 'These ratios pair separate processes in reversed source-build order, selecting loop-memory-base ON in both binaries;'))
