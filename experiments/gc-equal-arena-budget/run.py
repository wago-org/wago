#!/usr/bin/env python3
"""One-binary fixed-total arena sensitivity; no V8 comparison."""
import argparse, datetime, hashlib, json, os, pathlib, platform, subprocess, time
p=argparse.ArgumentParser()
p.add_argument('--repo',required=True);p.add_argument('--binary',required=True);p.add_argument('--output',required=True);p.add_argument('--audit',action='store_true')
a=p.parse_args();repo=pathlib.Path(a.repo).resolve();binary=pathlib.Path(a.binary).resolve();out=pathlib.Path(a.output);out.mkdir(parents=True,exist_ok=True)
def sha(p):return hashlib.sha256(p.read_bytes()).hexdigest()
cases=json.loads((repo/'bench/experiments/gc-v8/cases.json').read_text())
names=['struct-allocation-access','array-allocation-access','array-fill','array-copy-checksum','array-loop-copy-checksum','tree-survival']
cases=[next(c for c in cases if c['name']==name) for name in names]
variants=[('young2-old8',2,8),('young4-old7',4,7),('young6-old6',6,6)]
provenance={'source_commit':subprocess.check_output(['git','rev-parse','HEAD'],cwd=repo,text=True).strip(),'production_source':'2a907dab','binary_sha256':sha(binary),'binary_bytes':binary.stat().st_size,'platform':platform.platform(),'cpu':pathlib.Path('/proc/cpuinfo').read_text(),'protocol':{'warmup':512 if a.audit else 100,'samples':0 if a.audit else 9,'batch':32,'rounds':1 if a.audit else 3,'affinity_pinned':False,'gomaxprocs':1},'tags':'wago_gc_direct_trusted_loop,wago_gc_direct_trusted_refstruct,wago_gc_direct_trusted_alloc,wago_gc_direct_fill_sse2,wago_gc_direct_copy,wago_gc_direct_array_alloc,wago_gc_direct_weighted_sum','scope':'same-binary policy sensitivity; own local jobs paused, external host contention not excluded; no V8 claim'}
rows=[]
for rnd in range(provenance['protocol']['rounds']):
 for c in cases[rnd:]+cases[:rnd]:
  for label,young,old in variants[rnd:]+variants[:rnd]:
   env={k:v for k,v in os.environ.items() if not k.startswith('GC_BRIDGE_')}
   env.update(GOMAXPROCS='1',GC_BRIDGE_AGE2='0',GC_BRIDGE_CASE=c['name'],GC_BRIDGE_MODE='direct',GC_BRIDGE_COMPILER_MODE='fixed-total-policy',GC_BRIDGE_EDEN=str(young<<20),GC_BRIDGE_OLD=str(old<<20),GC_BRIDGE_WARMUP=str(provenance['protocol']['warmup']),GC_BRIDGE_SAMPLES=str(provenance['protocol']['samples']),GC_BRIDGE_BATCH='32')
   row={'round':rnd,'case':c['name'],'variant':label,'started_utc':datetime.datetime.now(datetime.timezone.utc).isoformat(),'load_before':os.getloadavg(),'allowed_affinity':sorted(os.sched_getaffinity(0))}
   start=time.monotonic();r=subprocess.run([str(binary),'-test.run=^TestGCDirectBridgeProbe$','-test.v','-test.count=1'],cwd=repo/'src/wago',env=env,capture_output=True,text=True)
   row.update(ended_utc=datetime.datetime.now(datetime.timezone.utc).isoformat(),wall_seconds=time.monotonic()-start,load_after=os.getloadavg(),returncode=r.returncode,stdout=r.stdout.replace(str(repo),'REPO'),stderr=r.stderr.replace(str(repo),'REPO'))
   for line in r.stdout.splitlines():
    if 'DIRECT_BRIDGE_RESULT ' in line:row['result']=json.loads(line.split('DIRECT_BRIDGE_RESULT ',1)[1])
   rows.append(row);(out/'results.json').write_text(json.dumps({'provenance':provenance,'rows':rows},indent=2));print(rnd,c['name'],label,r.returncode,flush=True)
   if r.returncode or 'result' not in row:raise SystemExit('probe failure; evidence retained')
   v=row['result'];g=v['actual_direct_geometry']
   assert v['artifact_sha256']==c['sha256'] and v['checksum']==c['want']
   assert v['native_direct_view_installed'] and not v['allocation_elision']
   assert g['nursery_bytes']==young<<20 and g['each_old_bytes']==old<<20 and g['arena_bytes']==16+(18<<20) and g['derived_fixed_bytes']==28463151
   assert v['config']['ThroughputHeapBytes']==old<<20 and not v['config']['ExperimentalCompactAge2'] and not v['config']['ExperimentalCompactOffsets']
