#!/usr/bin/env python3
"""Read-only pin exports + untimed review-driven workload tests; failures retained."""
import pathlib,subprocess,tarfile,io,os,json,hashlib,signal
H=pathlib.Path(__file__).resolve().parent;R=H.parents[2];O=H/'.prepared';V=H/'additions-v3';V.mkdir(exist_ok=True)
GO='/home/dev/.local/share/mise/installs/go/1.27.1/bin/go';rows=[]
env={'PATH':str(pathlib.Path(GO).parent)+':/usr/bin:/bin','HOME':os.environ['HOME'],'GOROOT':str(pathlib.Path(GO).parents[1]),'GOTOOLCHAIN':'local','GOWORK':'off','GOMAXPROCS':'8','GOFLAGS':'-p=1','GOGC':'100','GOMEMLIMIT':'off','GODEBUG':'','GOEXPERIMENT':''}
def run(cmd,cwd,e,label):
 log=V/(label+'.log')
 with log.open('w') as f:
  p=subprocess.Popen(cmd,cwd=cwd,env=e,stdout=f,stderr=subprocess.STDOUT,start_new_session=True)
  try:code=p.wait(timeout=900)
  except subprocess.TimeoutExpired:os.killpg(p.pid,signal.SIGKILL);p.wait();code='TIMEOUT'
 rows.append(dict(argv=cmd,cwd=str(cwd),env=e,exit=code,log=str(log.relative_to(H)),sha256=hashlib.sha256(log.read_bytes()).hexdigest()))
 (V/'receipts.json').write_text(json.dumps(rows,indent=2)+'\n');print(label,code,flush=True);return code
pins=json.loads((H/'plan.json').read_text())['pins']
for pin in ['baseline','port']:
 tree=O/('review-'+pin);tree.mkdir(exist_ok=True)
 archive=subprocess.check_output(['git','archive',pins[pin],'--','.',':!docs'],cwd=R)
 with tarfile.open(fileobj=io.BytesIO(archive)) as tf:tf.extractall(tree,filter='data')
 for src,dst in [('review_guest_test.go.txt','src/wago/final_review_guest_test.go'),('domain_test.go.txt','src/wago/final_review_domain_test.go'),('review_native_test.go.txt','src/core/runtime/gc/native/final_review_native_test.go'),('census_test.go.txt','src/core/runtime/gc/native/final_review_census_test.go'),('design_review_probe_test.go.txt','src/wago/final_review_probe_test.go'),('design_review_cross_runtime_probe_test.go.txt','src/wago/final_review_cross_runtime_test.go')]:
  (tree/dst).write_bytes((H/'fixtures'/src).read_bytes())
 for mode in ['ordinary','wagodebug','wago_regalloccheck']:
  for pkg,rel in [('guest','src/wago'),('native','src/core/runtime/gc/native')]:
   binary=O/f'review-{pin}-{mode}-{pkg}.test'
   cmd=[GO,'test','-c','-o',str(binary)]+([] if mode=='ordinary' else ['-tags='+mode])+['./'+rel]
   if run(cmd,tree,env,f'{pin}-{mode}-{pkg}-build'):continue
   if pkg=='guest':
    for selector in ['TestFinalMeasurementPlainExtern','TestFinalMeasurementIndependentDomains','TestDesignReviewExternStorageImportsPrivateGCProducer','TestDesignReviewExternStorageProducerCrossRuntimeImport']:
     run([str(binary),'-test.run=^'+selector+'$','-test.v','-test.count=1','-test.timeout=5m'],tree/rel,env,f'{pin}-{mode}-{selector}')
   else:
    for shift in ([0] if pin=='baseline' else [0,12]):
     for edges in ['0','1']:
      for workers in ([1] if pin=='baseline' else [1,2,4,8]):
       run([str(binary),'-test.run=^TestFinalMeasurementLargeOldNurseryFill$','-test.v','-test.count=1','-test.timeout=5m'],tree/rel,env|{'FM_SHIFT':str(shift),'FM_WORKERS':str(workers),'FM_OBJECT_EDGES':edges},f'{pin}-{mode}-old-{shift}-{edges}-{workers}')
