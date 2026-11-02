import run as q
import sys,pathlib,json,subprocess,hashlib
modes=['ordinary','wagodebug','wago_regalloccheck']
def test(*a,**kw):
 rc=q.test(*a,**kw)
 if rc:raise SystemExit(rc)
lane=sys.argv[1]
if lane=='preflight':
 for mode in ['wago_regalloccheck','ordinary','wagodebug']:
  test('catalog-'+mode,['./src/wago'],mode,['-failfast','-run','^TestRFWStage3CorpusCompile$'],extra={'RFW_CATALOG':str(q.S/'corpus/catalog.json')})
 for mode in modes:
  test('parent-race-reduced-'+mode,['./src/wago'],mode,['-race','-failfast','-run','^TestPreparedGCRevocableEntryExternalAccess$/^reference-store$'])
elif lane=='baseline':
 b=pathlib.Path(q.M['scratch'])/'baseline';q.M['pin']=json.loads((q.E/'baseline.json').read_text())['pin']
 for mode in modes:
  test('baseline-corpus-'+mode,['./suite','-args','-wago.corpus=all'],mode,['-failfast','-run','^(TestCorpus|TestCorpusSemanticExec|TestCatalog.*|TestValidateCorpusModule.*|TestApplicationCorpusRuns)$'],b/'bench')
elif lane=='graphs':
 for label in ['port','baseline']:
  s=q.S
  if label=='baseline':
   s=pathlib.Path(q.M['scratch'])/'baseline';q.M['pin']=json.loads((q.E/'baseline.json').read_text())['pin'];overlays={}
   for rel in ['src/wago/rfwgc_stage3_measurement_bench_test.go','src/core/runtime/gc/native/rfwgc_stage3_measurement_bench_test.go']:
    data=(q.S/rel).read_bytes();(s/rel).write_bytes(data);overlays[rel]=hashlib.sha256(data).hexdigest()
   (q.E/'baseline-graph-overlay.json').write_text(json.dumps(overlays,indent=2))
  for mode in modes:
   out=q.E/('fixtures-'+label+'-'+mode);out.mkdir()
   test('graphs-'+label+'-'+mode,['./src/wago','./src/core/runtime/gc/native'],mode,['-failfast','-run','^TestRFWStage3Measurement'],s,{'RFW_EXPORT':str(out)})
elif lane=='diagnostics':
 for mode in modes:
  test('backend-'+mode,['./src/core/compiler/backend/...'],mode,['-failfast'])
  tags='wago_codegenstats'+('' if mode=='ordinary' else ','+mode)
  for route,extra in [('default',{}),('established',{'WAGO_SHARED_SCALAR':'0'})]:
   test('diagnostics-'+route+'-'+mode,['./src/core/compiler/backend/...','./src/wago'],tags,['-failfast'],extra=extra)
elif lane=='tinygo-extra':
 for mode in modes:
  tags=[] if mode=='ordinary' else ['-tags='+mode]
  for label,args in [('evacuation',['-run','Evacuation','./src/core/runtime/gc/native']),('joint',['-run','GCStage8TinyGo|TinyGoForeignStack','./src/core/runtime','./src/wago']),('conversion',['-run','^TestGCExternConversion','./src/wago'])]:
   if q.run('tinygo-'+label+'-'+mode,['tinygo','test','-scheduler=threads','-gc=conservative',*tags,'-v',*args]):raise SystemExit(1)
