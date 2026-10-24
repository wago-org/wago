#!/usr/bin/env python3
import hashlib,json,os,pathlib,re,statistics,subprocess,time
out=pathlib.Path(__file__).resolve().parent
root=out.parents[2]
bins={k:pathlib.Path('/tmp/wago-trapping-guard-'+k+'.test') for k in ('control','candidate')}
env=os.environ.copy();env['GOMAXPROCS']='1'
rows=[];runs=[];t0=time.monotonic()
for block in range(3):
 for n,mode in enumerate(('control','candidate','candidate','control')):
  name=f'micro-{block+1}-{n+1}-{mode}'
  cmd=['taskset','-c','15',str(bins[mode]),'-test.run=^$','-test.bench=^BenchmarkGCTrappingGuard$','-test.benchtime=200ms','-test.count=1']
  start=time.monotonic()
  with (out/(name+'.log')).open('w') as log:p=subprocess.run(cmd,cwd=root/'src/wago',env=env,stdout=log,stderr=subprocess.STDOUT)
  result={'mode':mode,'block':block+1,'order':n+1,'command':cmd,'seconds':time.monotonic()-start,'exit':p.returncode}
  runs.append(result)
  for line in (out/(name+'.log')).read_text().splitlines():
   m=re.match(r'BenchmarkGCTrappingGuard/([^\s-]+(?:-[^\s]+)?)\s+([0-9]+)\s+([0-9.]+) ns/op\s+([0-9]+) B/op\s+([0-9]+) allocs/op',line)
   if m:rows.append({**result,'case':m[1],'N':int(m[2]),'ns':float(m[3]),'bytes':int(m[4]),'allocs':int(m[5])})
  (out/'micro-samples.json').write_text(json.dumps({'runs':runs,'rows':rows},indent=2)+'\n')
  if p.returncode:raise SystemExit(f'Failed sample kept: {name}')
summary={}
for case in sorted({r['case'] for r in rows}):
 vals={k:[r['ns'] for r in rows if r['case']==case and r['mode']==k] for k in bins}
 if any(len(v)!=6 for v in vals.values()):raise SystemExit(f'Incomplete samples: {case} {vals}')
 med={k:statistics.median(v) for k,v in vals.items()}
 summary[case]={'ns':vals,'medians':med,'percent':100*(med['candidate']/med['control']-1)}
if len(summary)!=5:raise SystemExit(f'Expected5 cases, got{len(summary)}')
report={'seconds':time.monotonic()-t0,'binary_sha256':{k:hashlib.sha256(p.read_bytes()).hexdigest() for k,p in bins.items()},'summary':summary}
(out/'micro-summary.json').write_text(json.dumps(report,indent=2)+'\n');print(json.dumps(report,indent=2))
