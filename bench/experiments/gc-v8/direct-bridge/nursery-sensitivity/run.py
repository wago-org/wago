#!/usr/bin/env python3
import argparse,hashlib,json,os,pathlib,subprocess,time,platform
p=argparse.ArgumentParser();p.add_argument('binary');p.add_argument('--output-dir',required=True);p.add_argument('--audit',action='store_true');a=p.parse_args()
root=pathlib.Path(__file__).resolve().parents[5];binary=pathlib.Path(a.binary).resolve();out=pathlib.Path(a.output_dir).resolve();out.mkdir(parents=True,exist_ok=True)
h=hashlib.sha256()
with binary.open('rb')as f:
 for block in iter(lambda:f.read(262144),b''):h.update(block)
provenance={'source_commit':subprocess.check_output(['git','rev-parse','HEAD'],cwd=root,text=True).strip(),'binary_sha256':h.hexdigest(),'binary_bytes':binary.stat().st_size,'tag':'wago_gc_direct_refstore','platform':platform.platform(),'cpu':pathlib.Path('/proc/cpuinfo').read_text(),'started_utc':time.strftime('%Y-%m-%dT%H:%M:%SZ',time.gmtime()),'protocol':{'warmup':3,'samples':0 if a.audit else 1,'batch':5,'rounds':1 if a.audit else 3},'old_semispace_bytes':8<<20,'old_semispace_count':2,'caution':'nursery policy sensitivity, not matched codegen or V8 comparison'}
rows=[];cases=['struct-allocation-access','array-allocation-access','tree-survival'];edens=[64<<10,512<<10,2<<20];output=out/('audit.json'if a.audit else'results.json')
for round in range(1 if a.audit else 3):
 for case in cases[round:]+cases[:round]:
  for eden in edens[round:]+edens[:round]:
   env=dict(os.environ,GC_BRIDGE_CASE=case,GC_BRIDGE_MODE='direct',GC_BRIDGE_COMPILER_MODE='native_refstore',GC_BRIDGE_EDEN=str(eden),GC_BRIDGE_WARMUP='3',GC_BRIDGE_SAMPLES='0'if a.audit else'1',GC_BRIDGE_BATCH='5',GOMAXPROCS='1')
   r=subprocess.run([str(binary),'-test.run=^TestGCDirectBridgeProbe$','-test.v','-test.count=1'],cwd=root/'src/wago',env=env,text=True,capture_output=True)
   row={'round':round,'case':case,'eden_bytes':eden,'returncode':r.returncode,'stdout':r.stdout,'stderr':r.stderr}
   for line in r.stdout.splitlines():
    if 'DIRECT_BRIDGE_RESULT 'in line:row['result']=json.loads(line.split('DIRECT_BRIDGE_RESULT ',1)[1])
   rows.append(row);output.write_text(json.dumps({'provenance':provenance,'rows':rows},indent=2));print(round,case,eden,r.returncode,flush=True)
if any(r['returncode']or'result'not in r for r in rows):raise SystemExit(1)
