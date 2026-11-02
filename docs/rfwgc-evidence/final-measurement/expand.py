#!/usr/bin/env python3
"""Bind the logical plan to qualified binaries. Writes commands, never runs them.
Bindings: {"<target>/<pin>/<package>": {"binary":absolute_path,"cwd":absolute_path,
"correctness_receipt":absolute_path,"test_passes":{selector:[exact leaf names]}}}.
Unbound/unsupported rows remain explicit. A non-complete seal is not runnable.
"""
import argparse,hashlib,json,pathlib,random
H=pathlib.Path(__file__).resolve().parent

def digest(p):return hashlib.sha256(pathlib.Path(p).read_bytes()).hexdigest()
def main():
 ap=argparse.ArgumentParser();ap.add_argument('--bindings',required=True,type=pathlib.Path);ap.add_argument('--out',required=True,type=pathlib.Path);ap.add_argument('--serial-cpu',required=True,type=int);ap.add_argument('--parallel-cpus',required=True);a=ap.parse_args()
 p=json.loads((H/'plan.json').read_text());bindings=json.loads(a.bindings.read_text())
 cpus=a.parallel_cpus.split(',');assert len(cpus)==8 and len(set(cpus))==8
 rows=[];unbound=[]
 for c in p['cells']:
  row={'id':c['id'],'kind':c['kind'],'samples':{}}
  for side in ['A','B']:
   cfg=c[side];key='/'.join([c['target'],cfg['pin'],c['package']]);b=bindings.get(key)
   if b is None:unbound.append({'id':c['id'],'side':side,'binding':key});continue
   binary=pathlib.Path(b['binary']);cwd=pathlib.Path(b['cwd']);receipt=pathlib.Path(b['correctness_receipt'])
   assert binary.is_absolute() and binary.is_file() and cwd.is_absolute() and cwd.is_dir() and receipt.is_file()
   parallel=c['id'].startswith(('graph-workers/','evacuation/'))
   env={'GOMAXPROCS':'8' if parallel else '1','GOGC':'100','GOEXPERIMENT':'','GODEBUG':'','WAGO_SHARED_SCALAR':'1',**cfg['env']}
   selector=cfg.get('selector',c['selector'])
   cmd=['taskset','-c',a.parallel_cpus if parallel else str(a.serial_cpu),str(binary),'-test.count=1','-test.v','-test.timeout=3m']
   if c['kind']=='test-cycle':
    assert b.get('test_passes',{}).get(selector), 'freeze exact nonempty per-selector pass census before acquisition'
    cmd+=['-test.run='+selector]
   else:cmd+=['-test.run=^$','-test.bench='+selector,'-test.benchtime=500ms']
   if 'catalog_id' in c:cmd+=['-wago.corpus='+c['catalog_id']]
   row['samples'][side]={'argv':cmd,'cwd':str(cwd),'env':env,'binary_sha256':digest(binary),'receipt_sha256':digest(receipt),'expected_test_passes':b.get('test_passes',{}).get(selector,[])}
  rows.append(row)
 # Preserve predeclared priority strata, randomize within each reproducibly.
 rng=random.Random(p['seed']);ordered=[]
 for prefix in ['focused','corpus/compile/','corpus/runtime/']:
  group=[r for r in rows if (not r['id'].startswith('corpus/') if prefix=='focused' else r['id'].startswith(prefix))]
  rng.shuffle(group);ordered+=group
 candidate=json.loads((H/'candidate-status.json').read_text()) if (H/'candidate-status.json').exists() else {'status':'UNQUALIFIED'}
 result={'candidate_status':candidate['status'],'plan_sha256':digest(H/'plan.json'),'status':'INVALID' if candidate['status']=='INVALID' else ('UNBOUND' if unbound else 'NEEDS_LEAF_CENSUS_AND_OWNER_SEAL'),'cells':ordered,'unbound':unbound,'note':'This generator grants no lease and executes nothing. Check actual CPU topology and all leaf census/receipts before sealing.'}
 with a.out.open('x') as f:json.dump(result,f,indent=2);f.write('\n')
if __name__=='__main__':main()
