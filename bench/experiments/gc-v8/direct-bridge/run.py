#!/usr/bin/env python3
"""Run a prebuilt helper-only test binary in alternating fresh processes."""
import argparse,json,os,pathlib,subprocess
p=argparse.ArgumentParser();p.add_argument('binary');p.add_argument('--rounds',type=int,default=1);p.add_argument('--warmup',type=int,default=12);p.add_argument('--samples',type=int,default=0);p.add_argument('--batch',type=int,default=4);p.add_argument('--output',required=True);a=p.parse_args()
root=pathlib.Path(__file__).resolve().parents[4]
cases=['struct-allocation-access','array-allocation-access','array-fill','array-copy-checksum','array-loop-copy-checksum','tree-survival'];results=[]
for round in range(a.rounds):
 for case in cases[round%len(cases):]+cases[:round%len(cases)]:
  for mode in (['handles','direct'] if round%2==0 else ['direct','handles']):
   env=dict(os.environ,GC_BRIDGE_CASE=case,GC_BRIDGE_MODE=mode,GC_BRIDGE_WARMUP=str(a.warmup),GC_BRIDGE_SAMPLES=str(a.samples),GC_BRIDGE_BATCH=str(a.batch),GOMAXPROCS='1')
   r=subprocess.run([str(pathlib.Path(a.binary).resolve()),'-test.run=^TestGCDirectBridgeProbe$','-test.v','-test.count=1'],cwd=root/'src/wago',env=env,text=True,capture_output=True)
   row={'round':round,'case':case,'mode':mode,'returncode':r.returncode,'stdout':r.stdout,'stderr':r.stderr}
   for line in r.stdout.splitlines():
    if 'DIRECT_BRIDGE_RESULT ' in line:row['result']=json.loads(line.split('DIRECT_BRIDGE_RESULT ',1)[1])
   results.append(row);pathlib.Path(a.output).write_text(json.dumps(results,indent=2));print(case,mode,r.returncode,flush=True)
if any(r['returncode'] or 'result' not in r for r in results):raise SystemExit(1)
