#!/usr/bin/env python3
import run as q
import pathlib,tarfile,subprocess,json,hashlib,sys
q.env.pop('GOWORK',None)
for label,pin in [('port',q.M['pin']),('baseline','a3c08365f531eb3c9003e3acf3e0a822c12c31f4')]:
 s=q.S
 if label=='baseline':
  s=pathlib.Path(q.M['scratch'])/'graph-baseline';s.mkdir()
  with tarfile.open(pathlib.Path(q.M['scratch'])/'baseline.tar') as t:t.extractall(s,filter='data')
  hashes={}
  for rel in ['src/wago/rfwgc_stage3_measurement_bench_test.go','src/core/runtime/gc/native/rfwgc_stage3_measurement_bench_test.go']:
   data=(q.S/rel).read_bytes();(s/rel).write_bytes(data);hashes[rel]=hashlib.sha256(data).hexdigest()
  (q.E/'graph-baseline-adapter.json').write_text(json.dumps({'pin':pin,'test_only_overlay_from':q.M['pin'],'sha256':hashes,'source':str(s)},indent=2)+'\n')
 q.M['pin']=pin
 for m in ['ordinary','wagodebug','wago_regalloccheck']:
  output=q.E/('fixtures-'+label+'-'+m);output.mkdir()
  if q.test('graphs-and-explicit-catalog-'+label+'-'+m,['./src/wago','./src/core/runtime/gc/native'],m,['-failfast','-run','^TestRFWStage3(Measurement|CorpusCompile)'],s,{'RFW_CATALOG':str(s/'corpus/catalog.json'),'RFW_EXPORT':str(output)}):sys.exit(1)
