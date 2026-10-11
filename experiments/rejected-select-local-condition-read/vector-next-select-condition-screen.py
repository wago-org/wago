from pathlib import Path
import json,os,statistics,subprocess

work=Path('/tmp/wago-vector-next-37174169')
out=work/'experiments/arm64-parity/select-local-condition-read'
out.mkdir(parents=True,exist_ok=True)
env=dict(os.environ,WAGO_SHARED_SCALAR='0',WAGO_BOUNDS='signals')
def run(args,tag,extra=None):
 p=subprocess.run(args,cwd=work,env=env|dict(extra or {}),capture_output=True,text=True)
 (out/(tag+'.raw')).write_text(p.stdout+p.stderr)
 print(tag,p.returncode,flush=True)
 if p.returncode:raise RuntimeError(p.stdout+p.stderr)
 return [json.loads(x) for x in p.stdout.splitlines() if x.startswith('{')]
run(['go','test','-tags','wago_guardpage wago_codegenstats wago_regalloccheck','./src/core/compiler/backend/railshot/arm64','-run','^TestSelect','-count=1','-v'],'fixtures',{'WAGO_ARM64_EXPERIMENT_SELECT_LOCAL_CONDITION_READ':'1'})
run(['go','build','-tags','wago_guardpage','-o','/tmp/paired-select-local-condition-read','./experiments/arm64-parity/paired'],'build')
rows=[]
cache='/Users/work/Code/Web/wasm.fyi/.wasmbench/corpus-cache'
for phase,rounds,budget in [('exec',10,'250ms'),('compile',8,'150ms')]:
 args=['/tmp/paired-select-local-condition-read','-option','select-local-condition-read','-corpus',cache,'-core-exec' if phase=='exec' else '-core-compile','-workloads','modular-arithmetic','-phase',phase,'-rounds',str(rounds),'-budget',budget]
 if phase=='exec':args+=['-code-dir',str(out/'native-target')]
 rows+=run(args,'target-'+phase)
 (out/'target.jsonl').write_text(''.join(json.dumps(x)+'\n' for x in rows))
lines=['|workload|phase|median paired change|pairs|','|---|---|---:|---:|']
for name,phase in sorted({(x['workload'],x['phase']) for x in rows}):
 data={(x['round'],x['on']):x['us'] for x in rows if x['workload']==name and x['phase']==phase}
 ratios=[100*(data[(r,True)]/data[(r,False)]-1) for r,on in data if not on]
 lines.append(f'|{name}|{phase}|{statistics.median(ratios):+.2f}%|{len(ratios)}|')
(out/'target-summary.md').write_text('\n'.join(lines)+'\n')
print('\n'.join(lines),flush=True)
