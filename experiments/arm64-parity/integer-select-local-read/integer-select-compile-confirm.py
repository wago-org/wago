from pathlib import Path
import os,subprocess,json,re,statistics
out=Path('/tmp/integer-select-narrow');e=os.environ.copy();e.update(WAGO_SHARED_SCALAR='0',WAGO_ARM64_EXPERIMENT_INTEGER_SELECT_READ='1')
rows=[]
for kind,name in [('app','stats-quickselect'),('core','complex-roundtrip')]:
 cmd=['/tmp/paired-integer-select-narrow','-option','integer-select-read','-workloads',name,'-phase','compile','-rounds','8','-budget','150ms']
 if kind=='core':cmd+=['-corpus','/Users/work/Code/Web/wasm.fyi/.wasmbench/corpus-cache','-core-compile']
 p=out/('compile-confirm-'+name+'.raw')
 with p.open('w') as f:subprocess.run(cmd,env=e,stdout=f,stderr=subprocess.STDOUT,check=True)
 rows += [json.loads(l) for l in p.read_text().splitlines() if l.startswith('{')]
for pair in range(8):
 for on in [bool(pair%2),not bool(pair%2)]:
  ee=e|dict(WAGO_PARITY_CACHE='/Users/work/Code/Web/wasm.fyi/.wasmbench/corpus-cache',WAGO_PARITY_CORE_ID='wago/coremark/2k-performance',WAGO_ARM64_EXPERIMENT_INTEGER_SELECT_READ=str(int(on)))
  p=out/('compile-confirm-coremark-'+str(pair)+'-'+str(int(on))+'.raw')
  with p.open('w') as f:subprocess.run(['/tmp/parity-integer-select-narrow-signal.test','-test.run=^$','-test.bench=BenchmarkCachedContract/Compile$','-test.benchtime=150ms','-test.count=1'],env=ee,stdout=f,stderr=subprocess.STDOUT,check=True)
  m=re.search(r'BenchmarkCachedContract/Compile-\d+\s+\d+\s+([0-9.]+) ns/op',p.read_text());assert m
  rows.append(dict(workload='coremark',phase='compile',round=pair,on=on,us=float(m[1])/1000))
(out/'compile-confirm.jsonl').write_text(''.join(json.dumps(x)+'\n' for x in rows))
lines=['|workload|paired change|OFF us|ON us|','|---|---:|---:|---:|']
for name in sorted({r['workload'] for r in rows}):
 d={(r['round'],r['on']):r['us'] for r in rows if r['workload']==name}
 ratio=statistics.median(100*(d[(i,True)]/d[(i,False)]-1) for i in range(8))
 lines.append(f'|{name}|{ratio:+.2f}%|{statistics.median(d[(i,False)] for i in range(8)):.3f}|{statistics.median(d[(i,True)] for i in range(8)):.3f}|')
(out/'compile-confirm-summary.md').write_text('\n'.join(lines)+'\n');print('\n'.join(lines),flush=True)
