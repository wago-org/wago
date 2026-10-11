from pathlib import Path
import json,os,re,statistics,subprocess
work=Path('/tmp/wago-vector-next-37174169')
out=work/'experiments/arm64-parity/select-local-condition-read'
cache=Path('/Users/work/Code/Web/wasm.fyi/.wasmbench/corpus-cache')
env=dict(os.environ,WAGO_SHARED_SCALAR='0',WAGO_PARITY_CACHE=str(cache))
def run(args,tag,extra=None):
 p=subprocess.run(args,cwd=work,env=env|dict(extra or {}),text=True,capture_output=True)
 (out/(tag+'.raw')).write_text(p.stdout+p.stderr)
 if p.returncode:raise RuntimeError((tag,p.stdout,p.stderr))
 return p.stdout
run(['go','test','-tags','wago_guardpage wago_codegenstats wago_regalloccheck','./src/core/compiler/backend/railshot/arm64','-run','^TestSelectLocalConditionRead|^TestMatchingSelectedLocalRead','-count=1','-v'],'pressure-fixtures')
run(['go','test','-c','-tags','wago_guardpage','-o','/tmp/parity-select-local-condition-read.test','./experiments/arm64-parity'],'core-build')
files={r['name'] for r in map(json.loads,(out/'inventory.jsonl').read_text().splitlines()) if r['changed']}
manifest=json.loads((cache/'manifest.json').read_text())['workloads']
rows=[]
for w in manifest:
 if w['id'].replace('/','__')+'.bin' not in files:continue
 assert w['abi']=='core' and w['oracle']['kind']=='exact_u64',w['id']
 for phase in ['exec','compile']:
  for pair in range(4):
   for on in [bool(pair%2),not bool(pair%2)]:
    tag=w['id'].replace('/','__')+f'-{phase}-{pair}-{int(on)}'
    raw=run(['/tmp/parity-select-local-condition-read.test','-test.run=^$','-test.bench=BenchmarkCachedContract/'+phase.title()+'$','-test.benchtime=100ms','-test.count=1'],tag,{'WAGO_PARITY_CORE_ID':w['id'],'WAGO_ARM64_EXPERIMENT_SELECT_LOCAL_CONDITION_READ':str(int(on))})
    match=re.search(r'BenchmarkCachedContract/'+phase.title()+r'-\d+\s+\d+\s+([0-9.]+) ns/op',raw)
    assert match,raw
    rows.append(dict(workload=w['id'],phase=phase,round=pair,on=on,us=float(match[1])/1000))
    (out/'core-screen.jsonl').write_text(''.join(json.dumps(x)+'\n' for x in rows))
 print(w['id'],'done',flush=True)
lines=['Four reversed100ms separate-process pairs; execution verifies exact contracts. This is a short scope screen, not a same-process paired estimate.','', '|workload|phase|median paired change|pairs|','|---|---|---:|---:|']
for name,phase in sorted({(x['workload'],x['phase']) for x in rows}):
 data={(x['round'],x['on']):x['us'] for x in rows if x['workload']==name and x['phase']==phase}
 ratios=[100*(data[(r,True)]/data[(r,False)]-1) for r,on in data if not on]
 lines.append(f'|{name}|{phase}|{statistics.median(ratios):+.2f}%|{len(ratios)}|')
(out/'core-screen-summary.md').write_text('\n'.join(lines)+'\n')
print('\n'.join(lines),flush=True)
