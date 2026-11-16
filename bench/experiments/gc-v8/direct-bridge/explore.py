#!/usr/bin/env python3
"""Short explicitly exploratory direct-bridge run; no V8 target inference."""
import argparse,hashlib,json,os,pathlib,platform,subprocess,time
p=argparse.ArgumentParser();p.add_argument('helper_binary');p.add_argument('reads_binary');p.add_argument('--output',required=True);a=p.parse_args()
root=pathlib.Path(__file__).resolve().parents[4]
binaries={'helper_only':pathlib.Path(a.helper_binary).resolve(),'native_direct_reads':pathlib.Path(a.reads_binary).resolve()}
provenance={'source_commit':subprocess.check_output(['git','rev-parse','HEAD'],cwd=root,text=True).strip(),'platform':platform.platform(),'cpu':subprocess.check_output(['lscpu'],text=True),'binaries':{k:{'path':str(v),'bytes':v.stat().st_size,'sha256':hashlib.sha256(v.read_bytes()).hexdigest()}for k,v in binaries.items()},'warmup':3,'samples':1,'batch':5,'rounds':3,'warning':'exploratory short run; unequal physical heap budgets/policies; not V8 comparison','started_utc':time.strftime('%Y-%m-%dT%H:%M:%SZ',time.gmtime())}
rows=[];cases=['struct-allocation-access','array-allocation-access','tree-survival'];modes=[('helper_only','handles'),('helper_only','direct'),('native_direct_reads','direct')]
for round in range(3):
 for case in cases[round:]+cases[:round]:
  for compiler,mode in modes[round:]+modes[:round]:
   env=dict(os.environ,GC_BRIDGE_CASE=case,GC_BRIDGE_MODE=mode,GC_BRIDGE_COMPILER_MODE=compiler,GC_BRIDGE_WARMUP='3',GC_BRIDGE_SAMPLES='1',GC_BRIDGE_BATCH='5',GOMAXPROCS='1')
   result=subprocess.run([str(binaries[compiler]),'-test.run=^TestGCDirectBridgeProbe$','-test.v','-test.count=1'],cwd=root/'src/wago',env=env,text=True,capture_output=True)
   row={'round':round,'case':case,'mode':mode,'compiler':compiler,'returncode':result.returncode,'stdout':result.stdout,'stderr':result.stderr}
   for line in result.stdout.splitlines():
    if 'DIRECT_BRIDGE_RESULT ' in line:row['result']=json.loads(line.split('DIRECT_BRIDGE_RESULT ',1)[1])
   rows.append(row);pathlib.Path(a.output).write_text(json.dumps({'provenance':provenance,'rows':rows},indent=2));print(round,case,compiler,mode,result.returncode,flush=True)
if any(r['returncode'] or 'result'not in r for r in rows):raise SystemExit(1)
