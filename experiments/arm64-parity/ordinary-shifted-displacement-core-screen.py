from pathlib import Path
from statistics import median
import json,os,re,subprocess
root=Path('experiments/arm64-parity')
cache=Path('/Users/work/Code/Web/wasm.fyi/.wasmbench/corpus-cache')
manifest=json.loads((cache/'manifest.json').read_text())['workloads']
files=[x.split()[1] for x in (root/'ordinary-shifted-displacement-native-changes.txt').read_text().splitlines() if x.startswith('core ')]
rows=[]
for file in files:
 w=next(w for w in manifest if w['id'].replace('/','__')+'.bin'==file)
 assert w['abi']=='core' and w['oracle']['kind']=='exact_u64',w['id']
 for pair in range(2):
  for on in [bool(pair%2),not bool(pair%2)]:
   env=os.environ.copy();env.update(WAGO_SHARED_SCALAR='0',WAGO_PARITY_CACHE=str(cache),WAGO_PARITY_CORE_ID=w['id'],WAGO_ARM64_EXPERIMENT_ORDINARY_SHIFTED_IMMEDIATE=str(int(on)))
   raw=subprocess.run(['/tmp/parity-ordinary-shifted-displacement.test','-test.run=^$','-test.bench=BenchmarkCachedContract/Exec$','-test.benchtime=100ms','-test.count=1'],env=env,text=True,capture_output=True,check=True).stdout
   (root/('ordinary-shifted-displacement-core-'+w['id'].replace('/','__')+f'-{pair}-{int(on)}.txt')).write_text(raw)
   match=re.search(r'BenchmarkCachedContract/Exec-\d+\s+\d+\s+([0-9.]+) ns/op',raw);assert match,raw
   rows.append(dict(workload=w['id'],phase='exec',round=pair,on=on,us=float(match[1])/1000))
(root/'ordinary-shifted-displacement-core-screen.jsonl').write_text(''.join(json.dumps(x)+'\n' for x in rows))
