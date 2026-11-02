#!/usr/bin/env python3
"""Audit immutable exports; never consult dirty production content."""
import pathlib,subprocess,hashlib,json,os
H=pathlib.Path(__file__).resolve().parent;R=H.parents[2];P=json.loads((H/'plan.json').read_text())['pins'];rows=[]
allowed={'src/wago/rfwgc_stage3_measurement_bench_test.go','src/core/runtime/gc/native/rfwgc_stage3_measurement_bench_test.go'}
for name in ['baseline','port','shape-before','shape-after','size-shape-before','size-shape-after','diagnostic-source-baseline','diagnostic-source-port']:
 tree=H/'.prepared'/name
 if not tree.exists():continue
 pin=P[name.removeprefix('size-').removeprefix('diagnostic-source-')];drift=[];overlays={};checked=0;tracked=set()
 for rec in subprocess.check_output(['git','ls-tree','-rz',pin],cwd=R).split(b'\0'):
  if not rec:continue
  meta,path=rec.split(b'\t',1);mode,kind,want=meta.decode().split();path=path.decode()
  if kind!='blob' or path.startswith('docs/'):continue
  tracked.add(path);p=tree/path
  if not p.exists() and not p.is_symlink():drift.append({'path':path,'reason':'missing'});continue
  data=os.readlink(p).encode() if p.is_symlink() else p.read_bytes();actual=hashlib.sha1(b'blob '+str(len(data)).encode()+b'\0'+data).hexdigest();checked+=1
  if actual!=want:
   if path in allowed:overlays[path]=hashlib.sha256(data).hexdigest()
   else:drift.append({'path':path,'expected':want,'actual':actual})
 for p in tree.rglob('*'):
  if not p.is_file() or '.git' in p.parts:continue
  path=str(p.relative_to(tree))
  if path not in tracked:
   if path in allowed or (p.name.startswith('final_') and p.name.endswith('_test.go')):overlays[path]=hashlib.sha256(p.read_bytes()).hexdigest()
   else:drift.append({'path':path,'reason':'undeclared addition'})
 rows.append(dict(export=str(tree),pin=pin,tracked_blobs_checked=checked,drift=drift,test_overlays=overlays))
(H/'exports-integrity.json').write_text(json.dumps(rows,indent=2)+'\n')
print(json.dumps({'exports':len(rows),'blobs':sum(r['tracked_blobs_checked'] for r in rows),'drift':sum(len(r['drift']) for r in rows)}))
assert not any(r['drift'] for r in rows)
# Both paired adapters, stage3 fixtures and complete corpus files must match bytes.
a=H/'.prepared/baseline';b=H/'.prepared/port';matched=[];different=[]
for p in a.rglob('*'):
 if not p.is_file():continue
 rel=p.relative_to(a)
 if str(rel).startswith(('corpus/','bench/')) or p.name.startswith('final_review') or p.name in ['final_measurement_graph_test.go','rfwgc_stage3_measurement_bench_test.go']:
  q=b/rel
  if q.exists() and p.read_bytes()==q.read_bytes():matched.append(str(rel))
  else:different.append(str(rel))
(H/'paired-workload-identity.json').write_text(json.dumps({'matched_paths':matched,'different_paths':different,'policy':'Differences require explicit review; runtime implementation differs intentionally.'},indent=2)+'\n')
assert not different,different
