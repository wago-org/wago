#!/usr/bin/env python3
import json,pathlib,hashlib
H=pathlib.Path(__file__).resolve().parent;P=json.loads((H/'plan.json').read_text());B=json.loads((H/'qualification/builds.json').read_text());rows=[];artifacts=[]
for pin in ['baseline','port']:
 tree=H/'.prepared'/pin;cat=json.loads((tree/'corpus/catalog.json').read_text())
 assert hashlib.sha256((tree/'corpus/catalog.json').read_bytes()).hexdigest()==P['catalog_sha256']
 assert len(cat['benchmarks'])==128
 for e in cat['benchmarks']:
  path=tree/'corpus'/e['artifact'];actual=hashlib.sha256(path.read_bytes()).hexdigest() if path.is_file() else None
  artifacts.append(dict(pin=pin,id=e['id'],path=str(path),expected=e['artifact_sha256'],actual=actual,match=actual==e['artifact_sha256']))
 for c in P['cells']:
  if c['package']!='bench':continue
  cfg=c['A' if pin=='baseline' else 'B'];identity=json.dumps([c['selector'],cfg['env'],c['catalog_id']],sort_keys=True)
  for mode,build in B['native-linux-amd64/'+pin+'/bench'].items():
   check=build['configs'][identity]
   rows.append(dict(id=c['catalog_id'],pin=pin,source=P['pins'][pin],mode=mode,kind=c['kind'],status=check['status'],configuration_identity=identity,test_leaves=check.get('test_leaves',[]),benchmark_leaves=check.get('benchmark_leaves',[]),log=check.get('receipt',{}).get('log'),binary_sha256=build['sha256'],external_runtime='CAPTURED_REFERENCE_ONLY',gc_graph=False))
assert len(rows)==1536
(H/'fresh-corpus-census.json').write_text(json.dumps({'rows':rows,'artifacts':artifacts,'catalog_ids':128,'unique_artifact_hashes':len({r['expected'] for r in artifacts}),'mode_revision_kind_rows':len(rows),'inherited_manifest':'census.json is the original input identity ledger; these rows contain fresh correctness outcomes'},indent=2)+'\n')
assert all(r['status']=='PASS' for r in rows)
assert all(a['match'] for a in artifacts)
print('PASS: 128 IDs, 1536 fresh correctness rows, matched corpus artifact identities')
