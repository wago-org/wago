#!/usr/bin/env python3
"""Correctness only: validate evacuation Off/On and worker graph matrix in 3 modes."""
import importlib.util,json,pathlib,subprocess,os
H=pathlib.Path(__file__).resolve().parent
spec=importlib.util.spec_from_file_location('prep',H/'prepare.py');p=importlib.util.module_from_spec(spec);spec.loader.exec_module(p)
out=H/'.prepared';tree=out/'port'; rel='src/core/runtime/gc/native'
(tree/rel/'final_measurement_evacuation_test.go').write_bytes((H/'fixtures/evacuation_test.go.txt').read_bytes())
env=os.environ|{'GOROOT':str(pathlib.Path(p.GO).parents[1]),'GOTOOLCHAIN':'local','GOWORK':'off','GOMAXPROCS':'8','GOFLAGS':'-p=1'}
rows=[]
for mode in p.MODES:
 binary=out/f'port-{mode}-extended.test'
 cmd=[p.GO,'test','-c','-o',str(binary)]+([] if mode=='ordinary' else ['-tags='+mode])+['./'+rel]
 with (out/(binary.name+'.build.log')).open('w') as log:subprocess.run(cmd,cwd=tree,env=env,stdout=log,stderr=subprocess.STDOUT,check=True)
 for w in [1,2,4,8]:
  for on in ['Off','On']:
   label=f'{mode}-{w}-{on}'
   cmd=[str(binary),'-test.run=^(TestFinalMeasurementEvacuation|TestFinalMeasurementGraph)$','-test.count=1','-test.v','-test.timeout=5m']
   with (out/(label+'.extra.log')).open('w') as log:r=subprocess.run(cmd,cwd=tree/rel,env=env|{'FM_WORKERS':str(w),'FM_SHIFT':'12','FM_EVAC':on},stdout=log,stderr=subprocess.STDOUT)
   rows.append({'mode':mode,'workers':w,'evacuation':on,'exit':r.returncode,'binary_sha256':p.sha(binary.read_bytes()),'command':cmd})
   (H/'extra-validation.json').write_text(json.dumps(rows,indent=2)+'\n')
   if r.returncode:raise SystemExit(r.returncode)
