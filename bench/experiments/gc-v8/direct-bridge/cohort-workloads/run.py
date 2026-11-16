#!/usr/bin/env python3
"""Untimed diagnostic only. Diagnostic traversal invalidates speed comparisons."""
import argparse, hashlib, json, os, pathlib, subprocess, time
p=argparse.ArgumentParser(); p.add_argument('--binary', required=True); p.add_argument('--output', required=True); a=p.parse_args()
root=pathlib.Path(__file__).resolve().parents[5]; binary=pathlib.Path(a.binary).resolve(); output=pathlib.Path(a.output); output.parent.mkdir(parents=True, exist_ok=True)
h=hashlib.sha256()
with binary.open('rb') as f:
    for b in iter(lambda:f.read(262144), b''): h.update(b)
provenance={'source':subprocess.check_output(['git','rev-parse','HEAD'],cwd=root,text=True).strip(), 'binary_sha256':h.hexdigest(), 'tags':'wago_gc_compact_range_loop,wago_gc_compact_refstruct,wago_gc_compact_cohort', 'untimed':True,'warmup':128,'following_calls':5,'eden':2097152,'old_semispace_bytes':8388608,'old_semispaces':2,'started_utc':time.strftime('%Y-%m-%dT%H:%M:%SZ',time.gmtime())}
rows=[]
for case in ['struct-allocation-access','array-allocation-access','array-fill','array-copy-checksum','array-loop-copy-checksum','tree-survival']:
    env=dict(os.environ,GOMAXPROCS='1',GC_BRIDGE_CASE=case,GC_BRIDGE_MODE='compact',GC_BRIDGE_EDEN='2097152',GC_BRIDGE_COMPILER_MODE='range_cohort',GC_BRIDGE_ALLOW_ZERO_COLLECTION='0')
    r=subprocess.run([str(binary),'-test.run=^TestGCCompactCohortProbe$','-test.v','-test.count=1'],cwd=root/'src/wago',env=env,text=True,capture_output=True)
    row={'case':case,'returncode':r.returncode,'stdout':r.stdout,'stderr':r.stderr}
    for line in r.stdout.splitlines():
        if 'COMPACT_COHORT_RESULT ' in line: row['result']=json.loads(line.split('COMPACT_COHORT_RESULT ',1)[1])
    rows.append(row);output.write_text(json.dumps({'provenance':provenance,'rows':rows},indent=2));print(case,r.returncode,flush=True)
    if r.returncode or 'result' not in row: raise SystemExit('Diagnostic failed; raw results retained')
