#!/usr/bin/env python3
import argparse,hashlib,json,os,pathlib,subprocess,time,platform
p=argparse.ArgumentParser();p.add_argument('--audit',action='store_true');p.add_argument('--binary-dir',required=True);p.add_argument('--output-dir',required=True);a=p.parse_args()
root=pathlib.Path(__file__).resolve().parents[5];base=pathlib.Path(a.binary_dir).resolve();out=pathlib.Path(a.output_dir).resolve();out.mkdir(parents=True,exist_ok=True)
variants=[('checked_refstore','wago_gc_direct_refstore'),('scalar_arrays','wago_gc_direct_scalar_arrays')]
def digest(path):
 h=hashlib.sha256()
 with path.open('rb')as f:
  for block in iter(lambda:f.read(262144),b''):h.update(block)
 return h.hexdigest()
provenance={'source_commit':subprocess.check_output(['git','rev-parse','HEAD'],cwd=root,text=True).strip(),'started_utc':time.strftime('%Y-%m-%dT%H:%M:%SZ',time.gmtime()),'platform':platform.platform(),'cpu':pathlib.Path('/proc/cpuinfo').read_text(),'variants':{},'protocol':{'warmup':3,'samples':0 if a.audit else 1,'batch':5,'rounds':1 if a.audit else 3},'caution':'matched checked scalar-array comparison at explicit 512 KiB, no V8 target inference'}
for label,tag in variants:
 binary=base/(label+'.test');provenance['variants'][label]={'tag':tag,'sha256':digest(binary),'bytes':binary.stat().st_size}
rows=[];cases=['struct-allocation-access','array-allocation-access','tree-survival'];output=out/('audit.json'if a.audit else'results.json')
for round in range(1 if a.audit else 3):
 for case in cases[round%3:]+cases[:round%3]:
  edens=[524288]
  for eden in edens:
   for label,tag in variants[round%2:]+variants[:round%2]:
    env=dict(os.environ,GC_BRIDGE_CASE=case,GC_BRIDGE_MODE='direct',GC_BRIDGE_COMPILER_MODE=label,GC_BRIDGE_EDEN=str(eden),GC_BRIDGE_WARMUP='3',GC_BRIDGE_SAMPLES='0'if a.audit else'1',GC_BRIDGE_BATCH='5',GOMAXPROCS='1')
    r=subprocess.run([str(base/(label+'.test')),'-test.run=^TestGCDirectBridgeProbe$','-test.v','-test.count=1'],cwd=root/'src/wago',env=env,text=True,capture_output=True)
    row={'round':round,'case':case,'eden_bytes':eden,'variant':label,'returncode':r.returncode,'stdout':r.stdout,'stderr':r.stderr}
    for line in r.stdout.splitlines():
     if 'DIRECT_BRIDGE_RESULT 'in line:row['result']=json.loads(line.split('DIRECT_BRIDGE_RESULT ',1)[1])
    rows.append(row);output.write_text(json.dumps({'provenance':provenance,'rows':rows},indent=2));print(round,case,eden,label,r.returncode,flush=True)
if any(r['returncode']or'result'not in r for r in rows):raise SystemExit(1)
