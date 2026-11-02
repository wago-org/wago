#!/usr/bin/env python3
"""Independent correctness lanes; archives never substitute working-tree source."""
import run as q
import pathlib,sys,subprocess,json,tarfile,hashlib
q.env.pop('GOWORK',None)
lane=sys.argv[1]
if lane=='tinygo':
 # Preserve original 34-command driver verbatim, including source selection via env.
 repo=q.E.parents[2];driver=repo/'docs/rfwgc-evidence/tinygo-foreign-stack-fix/qualify.sh'
 out=q.E/'tinygo-34';out.mkdir()
 (out/'driver.sh').write_bytes(driver.read_bytes())
 q.run('tinygo-34-driver',['bash',str(out/'driver.sh')],repo,{'WAGO_QUALIFICATION_HEAD':q.M['pin'],'WAGO_EVIDENCE_DIR':str(out)})
elif lane=='evacuation':
 for m in ['ordinary','wagodebug','wago_regalloccheck']:
  tags=[] if m=='ordinary' else ['-tags='+m]
  for label,args in [('evacuation',['-run','Evacuation','./src/core/runtime/gc/native']),('joint',['-run','GCStage8TinyGo|TinyGoForeignStack','./src/core/runtime','./src/wago'])]:
   if q.run('tinygo-'+label+'-'+m,['tinygo','test','-scheduler=threads','-gc=conservative',*tags,'-v',*args]):sys.exit(1)
elif lane=='hardening':
 for m in ['ordinary','wagodebug','wago_regalloccheck']:
  for guard in ['',',wago_guardpage']:
   tags=(m if m!='ordinary' else '')+guard;tags=tags.strip(',') or 'ordinary'
   if q.test('hardening-'+tags,['./src/core/runtime/gc/...'],tags,['-failfast','-count=3']):sys.exit(1)
   if q.test('host-transition-'+tags,['./src/wago'],tags,['-failfast','-count=3','-run','Test(RuntimeGCHostFuncRefCallAndTailOwnership|GenericGCResultsIssueBoundedHostTokens|GCCrossInstanceCallsWithSharedPersistentRoots|GCHostReentryNativeFrameRoots|GCArm64HostReentryRoots|GCWarmSnapshotPreservesCyclesAndSharing)$']):sys.exit(1)
elif lane=='baseline':
 pin='a3c08365f531eb3c9003e3acf3e0a822c12c31f4';b=pathlib.Path(q.M['scratch'])/'baseline';b.mkdir()
 archive=b.parent/'baseline.tar'
 with archive.open('wb') as f:subprocess.run(['git','archive',pin],cwd=q.E.parents[2],stdout=f,check=True)
 with tarfile.open(archive) as t:t.extractall(b,filter='data')
 (q.E/'baseline.json').write_text(json.dumps({'pin':pin,'archive_sha256':hashlib.sha256(archive.read_bytes()).hexdigest(),'source':str(b),'catalog_equal':(b/'corpus/catalog.json').read_bytes()==(q.S/'corpus/catalog.json').read_bytes()},indent=2)+'\n')
 q.M['pin']=pin
 for m in ['ordinary','wagodebug','wago_regalloccheck']:
  if q.test('baseline-corpus-'+m,['./suite','-args','-wago.corpus=all'],m,['-failfast','-run','^(TestCorpus|TestCorpusSemanticExec|TestApplicationCorpusRuns)$'],b/'bench'):sys.exit(1)
elif lane=='diagnostics':
 for m in ['wago_codegenstats','wagodebug,wago_codegenstats','wago_regalloccheck,wago_codegenstats']:
  q.test('diagnostics-'+m,['./src/core/compiler/backend/railshot/...','./src/wago'],m,['-failfast'],extra={'WAGO_SHARED_SCALAR':'0'})
elif lane=='fuzz':
 for pkg,name in [('./src/core/runtime/gc/native','FuzzStructCardDifferential'),('./tests/support/regressioncorpus','FuzzValidateRelativePathAndRustScannerDoNotPanic'),('./tests/tools/regression-corpus','FuzzNormalizeWABTJSONDoesNotPanic'),('./src/wago','FuzzSpecTrapMatchingDoesNotPanic'),('./src/core/compiler/wasm','FuzzDecodeValidateByteBackedDifferentialGenerated')]:
  if q.test('fuzz-'+name,[pkg],'wago_regalloccheck',['-run','^$','-fuzz','^'+name+'$','-fuzztime=5s','-parallel=2']):sys.exit(1)
