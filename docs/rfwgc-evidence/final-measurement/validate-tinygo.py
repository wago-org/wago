#!/usr/bin/env python3
"""Actual compiler, correctness only. Idle-stack maximum matrix point, all modes."""
import os,pathlib,subprocess,json,hashlib
H=pathlib.Path(__file__).resolve().parent;tree=H/'.prepared/port'
tool='/home/dev/.local/share/wago-tinygo-investigation/tinygo/bin/tinygo'
go='/home/dev/.local/share/mise/installs/go/1.27.1'
(tree/'src/core/runtime/gc/native/final_measurement_evacuation_test.go').write_bytes((H/'fixtures/evacuation_test.go.txt').read_bytes())
(tree/'src/core/runtime/final_measurement_foreign_stack_test.go').write_bytes((H/'fixtures/foreign_stack_test.go.txt').read_bytes())
env=os.environ|{'GOROOT':go,'GOTOOLCHAIN':'local','GOWORK':'off','GOMAXPROCS':'2','GOFLAGS':'-p=1','PATH':str(pathlib.Path(tool).parent)+':'+go+'/bin:'+os.environ['PATH'],'XDG_CACHE_HOME':str(H/'.prepared/tinygo-cache'),'FM_ENGINES':'8','FM_STACK_BYTES':str(8<<20)}
rows=[]
for mode in ['ordinary','wagodebug','wago_regalloccheck']:
 cmd=[tool,'test','-scheduler=threads','-gc=conservative']+([] if mode=='ordinary' else ['-tags='+mode])+['-v','-run','^TestFinalMeasurementIdleStacks$','./src/core/runtime']
 with (H/'.prepared'/('tinygo-'+mode+'.check.log')).open('w') as log:r=subprocess.run(cmd,cwd=tree,env=env,stdout=log,stderr=subprocess.STDOUT,timeout=900)
 raw=(H/'.prepared'/('tinygo-'+mode+'.check.log')).read_text()
 rows.append({'mode':mode,'command':cmd,'exit':r.returncode,'test_pass': '--- PASS: TestFinalMeasurementIdleStacks' in raw,'tool_sha256':hashlib.sha256(pathlib.Path(tool).read_bytes()).hexdigest(),'engines':8,'stack_bytes':8<<20})
 (H/'tinygo-validation.json').write_text(json.dumps(rows,indent=2)+'\n')
 if r.returncode or not rows[-1]['test_pass']:raise SystemExit(1)

for mode in ['ordinary','wagodebug','wago_regalloccheck']:
 for on in ['Off','On']:
  cmd=[tool,'test','-scheduler=threads','-gc=conservative']+([] if mode=='ordinary' else ['-tags='+mode])+['-v','-run','^TestFinalMeasurementEvacuation$','./src/core/runtime/gc/native']
  logpath=H/'.prepared'/('tinygo-evac-'+mode+'-'+on+'.check.log')
  with logpath.open('w') as log:r=subprocess.run(cmd,cwd=tree,env=env|{'FM_WORKERS':'1','FM_SHIFT':'12','FM_EVAC':on},stdout=log,stderr=subprocess.STDOUT,timeout=900)
  rows.append({'mode':mode,'command':cmd,'exit':r.returncode,'test_pass':'--- PASS: TestFinalMeasurementEvacuation' in logpath.read_text(),'evacuation':on,'workers':1,'shift':12})
  (H/'tinygo-validation.json').write_text(json.dumps(rows,indent=2)+'\n')
  if r.returncode or not rows[-1]['test_pass']:raise SystemExit(1)
