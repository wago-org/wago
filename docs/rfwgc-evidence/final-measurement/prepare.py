#!/usr/bin/env python3
"""Correctness only. Immutable exports; never runs Benchmark functions or clocks."""
import argparse, hashlib, io, json, os, pathlib, subprocess, tarfile
HERE=pathlib.Path(__file__).resolve().parent
ROOT=HERE.parents[2]
PINS={'baseline':'a3c08365f531eb3c9003e3acf3e0a822c12c31f4','port':'5345bd365c13963f1bf60e26bb80789ab9ead4cc'}
MODES=['ordinary','wagodebug','wago_regalloccheck']
GO='/home/dev/.local/share/mise/installs/go/1.27.1/bin/go'
def blob(pin,path): return subprocess.check_output(['git','show',pin+':'+path],cwd=ROOT)
def sha(data): return hashlib.sha256(data).hexdigest()
def main():
 ap=argparse.ArgumentParser();ap.add_argument('out',type=pathlib.Path);ap.add_argument('--build-and-check',action='store_true');a=ap.parse_args()
 out=a.out.resolve();out.relative_to(HERE);out.mkdir()
 env=os.environ|{'GOROOT':str(pathlib.Path(GO).parents[1]),'GOTOOLCHAIN':'local','GOWORK':'off','GOMAXPROCS':'2','GOFLAGS':'-p=1'}
 record={'pins':PINS,'overlays':{},'binaries':{},'checks':[]}
 for label,pin in PINS.items():
  tree=out/label;tree.mkdir()
  archive=subprocess.check_output(['git','archive',pin],cwd=ROOT)
  with tarfile.open(fileobj=io.BytesIO(archive)) as tf:tf.extractall(tree,filter='data')
  overlays={p:blob(PINS['port'],p) for p in ['src/wago/rfwgc_stage3_measurement_bench_test.go','src/core/runtime/gc/native/rfwgc_stage3_measurement_bench_test.go']}
  overlays['src/core/runtime/gc/native/final_measurement_graph_test.go']=(HERE/'fixtures/graph_test.go.txt').read_bytes()
  for p,data in overlays.items():(tree/p).write_bytes(data);record['overlays'][p]=sha(data)
  if not a.build_and_check:continue
  for mode in MODES:
   for package,rel,selector in [('native','src/core/runtime/gc/native','^(TestFinalMeasurementGraph|TestRFWStage3MeasurementNative)$'),('guest','src/wago','^TestRFWStage3MeasurementGuest$')]:
    binary=out/f'{label}-{mode}-{package}.test'
    cmd=[GO,'test','-c','-o',str(binary)]+([] if mode=='ordinary' else ['-tags='+mode])+['./'+rel]
    with (out/(binary.name+'.build.log')).open('w') as log:subprocess.run(cmd,cwd=tree,env=env,stdout=log,stderr=subprocess.STDOUT,check=True)
    record['binaries'][binary.name]=sha(binary.read_bytes())
    # Baseline graph/control ONLY; never run full RFW catalog execution.
    cmd=[str(binary),'-test.run='+selector,'-test.count=1','-test.v','-test.timeout=5m']
    with (out/(binary.name+'.check.log')).open('w') as log:r=subprocess.run(cmd,cwd=tree/rel,env=env|{'FM_WORKERS':'1','FM_SHIFT':'0'},stdout=log,stderr=subprocess.STDOUT)
    record['checks'].append({'label':label,'mode':mode,'package':package,'command':cmd,'exit':r.returncode})
    (out/'identity.json').write_text(json.dumps(record,indent=2)+'\n')
    if r.returncode:raise SystemExit(r.returncode)
 (out/'identity.json').write_text(json.dumps(record,indent=2)+'\n')
if __name__=='__main__':main()
