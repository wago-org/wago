from pathlib import Path
import os,subprocess,re,json
root=Path(__file__).resolve().parents[4];out=Path(__file__).resolve().parent
records=[]
for sample in range(1,21):
 for label in (['baseline','candidate'] if sample%2 else ['candidate','baseline']):
  cmd=['taskset','-c','0-15',str(root/'.tmp/setup-cleanup-next'/f'wasi-{label}.test'),'-test.run','^$']
  env=dict(os.environ,GOMAXPROCS='16',GOGC='100',GOMEMLIMIT='off',GODEBUG='inittrace=1',WAGO_BOUNDS='signals')
  r=subprocess.run(cmd,cwd=root/'bench/suite',env=env,text=True,stdout=subprocess.PIPE,stderr=subprocess.STDOUT,check=True)
  with (out/f'provider-init-{label}.txt').open('a') as f:f.write(f'# sample {sample}\n'+r.stdout)
  lines=[l for l in r.stdout.splitlines() if l.startswith('init github.com/wago-org/wasi/internal/core ')]
  records.append({'sample':sample,'label':label,'command':cmd,'core_init':lines})
(out/'provider-init-summary.json').write_text(json.dumps(records,indent=2)+'\n')
for label in ['baseline','candidate']:
 print(label,records[0 if label=='baseline' else 1]['core_init'])
