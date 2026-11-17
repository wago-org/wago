#!/usr/bin/env python3
"""Paired official adapter protocol; preserve declared reset and oracle behavior."""
import argparse,hashlib,json,os,pathlib,random,subprocess
p=argparse.ArgumentParser()
p.add_argument('--contracts',required=True);p.add_argument('--baseline',required=True);p.add_argument('--candidate',required=True);p.add_argument('--output',required=True)
p.add_argument('--trials',type=int,default=7);p.add_argument('--samples',type=int,default=9);p.add_argument('--warmup',type=int,default=3);p.add_argument('--operations',type=int,default=128);p.add_argument('--cpu',type=int,default=2);p.add_argument('--seed',type=int,default=145);p.add_argument('--only',default='');p.add_argument('--control',action='store_true');a=p.parse_args()
rows=json.loads(pathlib.Path(a.contracts).read_text());rows=[r for r in rows if not a.only or r['id'] in a.only.split(',')]
for r in rows:
 assert hashlib.sha256(pathlib.Path(r['path']).read_bytes()).hexdigest()==r['artifact_sha256'],r['id']
 assert r['workload']['reset'] in ('stateless','fresh_instance_per_sample'),r['id']
variants=[('baseline',a.baseline),('candidate',a.candidate)]+([('control',a.baseline)] if a.control else [])
hashes={b:hashlib.sha256(pathlib.Path(b).read_bytes()).hexdigest() for _,b in variants};rng=random.Random(a.seed)
with open(a.output,'x') as out:
 for trial in range(a.trials):
  rng.shuffle(rows)
  for r in rows:
   order=list(variants);rng.shuffle(order)
   for variant,binary in order:
    requests=[{'version':1,'id':1,'method':'describe'},{'version':1,'id':2,'method':'prepare','prepare':{'artifact':r['path'],'artifact_sha256':r['artifact_sha256'],'workload':r['workload'],'profile':'timing'}},{'version':1,'id':3,'method':'run','run':{'scenario':'steady','samples':a.samples,'warmup':a.warmup,'operations':a.operations}},{'version':1,'id':4,'method':'close'}]
    z=dict(trial=trial,id=r['id'],variant=variant,binary_sha256=hashes[binary],artifact_sha256=r['artifact_sha256'],contract_sha256=r['contract_sha256'],scenario='steady',reset=r['workload']['reset'],requests=requests)
    try:
     result=subprocess.run(['taskset','-c',str(a.cpu),binary],input=''.join(json.dumps(x,separators=(',',':'))+'\n' for x in requests),capture_output=True,text=True,env={**os.environ,'GOMAXPROCS':'2'},timeout=300)
     responses=[json.loads(x) for x in result.stdout.splitlines()];z.update(returncode=result.returncode,responses=responses,stderr=result.stderr)
     assert result.returncode==0 and len(responses)==4 and all(x['status']=='ok' for x in responses),'adapter error'
     samples=responses[2]['samples'];assert len(samples)==a.samples+a.warmup,'sample count'
     expected=1 if r['workload']['reset']=='fresh_instance_per_sample' else a.operations
     assert all(s.get('verified') and s.get('operations')==expected for s in samples),'oracle or operation count'
     z['verified']=True
    except Exception as e:z.update(verified=False,error=str(e))
    out.write(json.dumps(z,separators=(',',':'))+'\n');out.flush()
