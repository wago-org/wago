#!/usr/bin/env python3
"""Verify archived tracked bytes; only named test overlays may differ."""
import hashlib,json,pathlib,subprocess,os
H=pathlib.Path(__file__).resolve().parent;R=H.parents[2];P=json.loads((H/'plan.json').read_text())['pins'];rows=[]
allowed={'src/wago/rfwgc_stage3_measurement_bench_test.go','src/core/runtime/gc/native/rfwgc_stage3_measurement_bench_test.go'}
for name,pin in [('baseline',P['baseline']),('port',P['port']),('shape-before',P['shape-before']),('shape-after',P['shape-after']),('review-baseline',P['baseline']),('review-port',P['port']),('size-shape-before',P['shape-before']),('size-shape-after',P['shape-after'])]:
 tree=H/'.prepared'/name
 if not tree.exists():continue
 records=subprocess.check_output(['git','ls-tree','-rz',pin],cwd=R).split(b'\0');drift=[];checked=0;overlays={}
 for record in records:
  if not record:continue
  metadata,path=record.split(b'\t',1);mode,kind,expected=metadata.decode().split();path=path.decode();p=tree/path
  if kind!='blob' or (name.startswith(('review-','size-')) and path.startswith('docs/')):continue
  if not p.exists() and not p.is_symlink():drift.append({'path':path,'reason':'missing'});continue
  data=os.readlink(p).encode() if p.is_symlink() else p.read_bytes();actual=hashlib.sha1(b'blob '+str(len(data)).encode()+b'\0'+data).hexdigest();checked+=1
  if actual!=expected:
   if path in allowed:overlays[path]=hashlib.sha256(data).hexdigest()
   else:drift.append({'path':path,'expected_git_blob':expected,'actual_git_blob':actual})
 for p in tree.rglob('final_*test.go'):overlays[str(p.relative_to(tree))]=hashlib.sha256(p.read_bytes()).hexdigest()
 rows.append({'export':str(tree),'pin':pin,'tracked_blobs_checked':checked,'drift':drift,'test_overlays':overlays})
(H/'exports-final-integrity.json').write_text(json.dumps(rows,indent=2)+'\n')
print(json.dumps({'exports':len(rows),'drift':sum(len(r['drift']) for r in rows),'blobs_checked':sum(r['tracked_blobs_checked'] for r in rows)}))
assert not any(r['drift'] for r in rows)
