#!/usr/bin/env python3
"""Alternate fresh processes; preserve all raw samples and failed runs."""
import argparse, hashlib, json, os, pathlib, statistics, subprocess
p=argparse.ArgumentParser();p.add_argument('--baseline',required=True);p.add_argument('--candidate',required=True);p.add_argument('--node',default='node');p.add_argument('--launches',type=int,default=5);p.add_argument('--out',default='results.json');a=p.parse_args()
if a.launches<1:p.error('launches must be positive')
root=pathlib.Path(__file__).resolve().parent
cases=json.loads((root/'cases.json').read_text());results=[]
for c in cases:
 for path,key in [(c['file'],'sha256'),(c['file'].replace('.wasm','.wat'),'sourceSha256')]:
  if hashlib.sha256((root/path).read_bytes()).hexdigest()!=c[key]:raise ValueError('digest mismatch: '+path)
for launch in range(a.launches):
 for c in cases:
  if c['name']=='access-scalar-baseline':continue
  engines=[('baseline',[str(pathlib.Path(a.baseline).resolve())]),('candidate',[str(pathlib.Path(a.candidate).resolve())]),('v8',[a.node,'--no-liftoff',str(root/'v8.cjs')])];engines=engines[launch%3:]+engines[:launch%3]
  for engine,cmd in engines:
   args=[str(root/c['file']),c['export'],str(c['n']),str(c['want'])]
   r=subprocess.run(cmd+args,text=True,capture_output=True,timeout=120,env={**os.environ,'GC_PROOFS':'off'})
   row=dict(launch=launch,case=c['name'],engine=engine,returncode=r.returncode,stderr=r.stderr)
   if r.returncode==0:row.update(json.loads(r.stdout));row['median_ns']=statistics.median(row['samples_ns'])
   results.append(row);pathlib.Path(a.out).write_text(json.dumps(results,indent=2)+'\n');print(json.dumps(row),flush=True)
if any(r['returncode'] for r in results):raise SystemExit('One or more workloads failed; no universal parity result')
