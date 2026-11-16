#!/usr/bin/env python3
"""Separate untimed sampled attribution; never benchmark profile elapsed times."""
import argparse,hashlib,json,os,pathlib,subprocess
p=argparse.ArgumentParser();p.add_argument('--binary',required=True);p.add_argument('--output',required=True);a=p.parse_args()
root=pathlib.Path(__file__).resolve().parents[6];binary=pathlib.Path(a.binary).resolve();out=pathlib.Path(a.output).resolve();out.mkdir(parents=True,exist_ok=True)
h=hashlib.sha256()
with binary.open('rb') as f:
 for block in iter(lambda:f.read(262144),b''):h.update(block)
(out/'provenance.json').write_text(json.dumps({'binary_sha256':h.hexdigest(),'runtime_source':'5dfe7d3ca8ed2528c6e7a5fed73a3ea4acd8ef06','warmup':128,'following_calls':512,'total_young_bytes':2097152,'old_each':8388608,'untimed_profile':True,'cpu':pathlib.Path('/proc/cpuinfo').read_text()},indent=2))
for case in ['struct-allocation-access','array-allocation-access']:
 for mode in ['baseline','age2']:
  name=case+'-'+mode;prof=out/(name+'.cpu.pprof');env=dict(os.environ,GOMAXPROCS='1',GC_BRIDGE_CASE=case,GC_BRIDGE_MODE='compact',GC_BRIDGE_AGE2='1' if mode=='age2' else '0',GC_BRIDGE_COMPILER_MODE=mode,GC_BRIDGE_EDEN='2097152',GC_BRIDGE_WARMUP='128',GC_BRIDGE_SAMPLES='1',GC_BRIDGE_BATCH='512',GC_BRIDGE_ALLOW_ZERO_COLLECTION='0')
  r=subprocess.run([str(binary),'-test.run=^TestGCDirectBridgeProbe$','-test.v','-test.count=1','-test.cpuprofile='+str(prof)],cwd=root/'src/wago',env=env,text=True,capture_output=True);(out/(name+'.run.txt')).write_text(r.stdout+r.stderr)
  if r.returncode:raise SystemExit(name+' failed')
  for kind,args in [('flat',['-top']),('cum',['-top','-cum'])]:
   r=subprocess.run(['/workspace/shared/toolchains/go127/bin/go','tool','pprof',*args,str(binary),str(prof)],text=True,capture_output=True);(out/(name+'.'+kind+'.txt')).write_text(r.stdout+r.stderr)
   if r.returncode:raise SystemExit('pprof failed')
  print(name,'PASS',flush=True)
