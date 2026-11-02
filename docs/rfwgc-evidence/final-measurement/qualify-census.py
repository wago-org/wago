#!/usr/bin/env python3
"""Diagnostic clones only; no timing benchmark body is modified or invoked."""
import pathlib,subprocess,os,json,hashlib
H=pathlib.Path(__file__).resolve().parent;O=H/'.prepared';V=H/'detailed-census';V.mkdir(exist_ok=True)
GO='/home/dev/.local/share/mise/installs/go/1.27.1/bin/go';rows=[]
env={'PATH':str(pathlib.Path(GO).parent)+':/usr/bin:/bin','HOME':os.environ['HOME'],'GOROOT':str(pathlib.Path(GO).parents[1]),'GOTOOLCHAIN':'local','GOWORK':'off','GOMAXPROCS':'8','GOFLAGS':'-p=1','GOGC':'100','GOMEMLIMIT':'off','GODEBUG':'','GOEXPERIMENT':''}
for pin in ['baseline','port']:
 tree=O/('review-'+pin);rel='src/core/runtime/gc/native'
 graph=(H/'fixtures/graph_test.go.txt').read_text().split('func Benchmark')[0].replace('fmGraph','fmDiagnosticGraph').replace('TestFinalMeasurementGraph','TestFinalDiagnosticGraph')
 graph=graph.replace('roots:=RefSliceRoots{parent}', 'roots:=RefSliceRoots{parent}; if tt,ok:=t.(*testing.T);ok {fmDetailedCensus(tt,c,"allocated-graph")}').replace('if err=c.Verify(roots); err!=nil {t.Fatal(err)}','if err=c.Verify(roots); err!=nil {t.Fatal(err)};if tt,ok:=t.(*testing.T);ok {fmDetailedCensus(tt,c,"collected-graph")}')
 (tree/rel/'final_diagnostic_graph_test.go').write_text(graph)
 if pin=='port':
  evac=(H/'fixtures/evacuation_test.go.txt').read_text().split('func Benchmark')[0].replace('fmEvac','fmDiagnosticEvac').replace('TestFinalMeasurementEvacuation','TestFinalDiagnosticEvacuation')
  evac=evac.replace('c.throughput.mature.pressure=true','if tt,ok:=t.(*testing.T);ok {fmDetailedCensus(tt,c,"allocated-evacuation")}; c.throughput.mature.pressure=true').replace('d:=c.CollectionDiagnostics()','if tt,ok:=t.(*testing.T);ok {fmDetailedCensus(tt,c,"collected-evacuation")}; d:=c.CollectionDiagnostics()')
  (tree/rel/'final_diagnostic_evacuation_test.go').write_text(evac)
 for mode in ['ordinary','wagodebug','wago_regalloccheck']:
  binary=O/f'diagnostic-{pin}-{mode}.test'
  commands=[('build',[GO,'test','-c','-o',str(binary)]+([] if mode=='ordinary' else ['-tags='+mode])+['./'+rel],env,tree)]
  configs=[(1,0,'Off')] if pin=='baseline' else [(1,0,'Off')]+[(w,12,on) for w in [1,2,4,8] for on in ['Off','On']]
  for w,shift,on in configs:
   selector='^TestFinalDiagnosticGraph$' if shift==0 else '^(TestFinalDiagnosticGraph|TestFinalDiagnosticEvacuation)$'
   commands.append((f'{w}-{shift}-{on}',[str(binary),'-test.run='+selector,'-test.v','-test.count=1','-test.timeout=5m'],env|{'FM_WORKERS':str(w),'FM_SHIFT':str(shift),'FM_EVAC':on},tree/rel))
  for name,cmd,e,cwd in commands:
   log=V/f'{pin}-{mode}-{name}.log'
   with log.open('w') as f:p=subprocess.run(cmd,cwd=cwd,env=e,stdout=f,stderr=subprocess.STDOUT,timeout=900)
   rows.append({'pin':pin,'mode':mode,'argv':cmd,'cwd':str(cwd),'env':e,'exit':p.returncode,'log':str(log.relative_to(H)),'log_sha256':hashlib.sha256(log.read_bytes()).hexdigest(),'binary_sha256':hashlib.sha256(binary.read_bytes()).hexdigest() if binary.exists() else None})
   (V/'receipts.json').write_text(json.dumps(rows,indent=2)+'\n');print(pin,mode,name,p.returncode,flush=True)
   if p.returncode:break
