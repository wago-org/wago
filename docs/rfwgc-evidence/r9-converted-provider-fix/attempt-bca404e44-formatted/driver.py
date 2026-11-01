#!/usr/bin/env python3
"""Immutable, stopped ownership probes. Nonzero suite exits remain FAIL."""
import argparse, hashlib, io, json, os, pathlib, subprocess, tarfile, tempfile
p=argparse.ArgumentParser()
p.add_argument('--pin', required=True)
p.add_argument('--out', type=pathlib.Path, required=True)
a=p.parse_args()
repo=pathlib.Path(subprocess.check_output(['git','rev-parse','--show-toplevel'],text=True).strip())
root=pathlib.Path(__file__).resolve().parent
driver=pathlib.Path(__file__).read_bytes()
hashof=lambda b:hashlib.sha256(b).hexdigest()
pin=subprocess.check_output(['git','rev-parse',a.pin+'^{commit}'],text=True).strip()
a.out.mkdir(parents=True,exist_ok=False)
source=pathlib.Path(tempfile.mkdtemp(prefix='wago-h2-stopped-'))
archive=subprocess.check_output(['git','archive',pin])
with tarfile.open(fileobj=io.BytesIO(archive)) as tf: tf.extractall(source,filter='data')
probes={}
for f in sorted((root/'testdata').glob('*.go')):
 data=f.read_bytes(); probes[f.name]=hashof(data)
 (source/'src/wago'/f.name).write_bytes(data)
go='/home/dev/.local/share/mise/installs/go/1.27.3/bin/go'
env=os.environ.copy(); env.update(GOTOOLCHAIN='local',GOWORK='off',GOROOT=str(pathlib.Path(go).parent.parent))
(a.out/'driver.py').write_bytes(driver)
(a.out/'provenance.json').write_text(json.dumps(dict(pin=pin,archive_sha256=hashof(archive),source=str(source),probe_sha256=probes,driver_sha256=hashof(driver),go=subprocess.check_output([go,'version'],text=True,env=env).strip()),indent=2)+'\n')
results=[]
for mode in ['ordinary','wagodebug','wago_regalloccheck']:
 cmd=[go,'test','./src/wago','-run','^TestR9(ConvertedProviderState|H2ConvertedAggregateAdmission)$','-count=1','-v','-timeout=3m']
 if mode!='ordinary':cmd+=['-tags',mode]
 (a.out/(mode+'.command.json')).write_text(json.dumps(cmd)+'\n')
 with (a.out/(mode+'.log')).open('w') as out:
  r=subprocess.run(cmd,cwd=source,env=env,stdout=out,stderr=subprocess.STDOUT,timeout=240)
 log=(a.out/(mode+'.log')).read_text()
 results.append(dict(mode=mode,exit=r.returncode,verdict='PASS' if r.returncode==0 else 'FAIL',fail_lines=[l.strip() for l in log.splitlines() if '--- FAIL:' in l],skip_lines=[l.strip() for l in log.splitlines() if '--- SKIP:' in l],stop_lines=[l.strip() for l in log.splitlines() if 'STOP ' in l]))
 (a.out/'results.json').write_text(json.dumps(results,indent=2)+'\n')
 print(mode,r.returncode,flush=True)
unchanged=pathlib.Path(__file__).read_bytes()==driver
(a.out/'driver-final.json').write_text(json.dumps(dict(unchanged=unchanged,driver_sha256=hashof(driver),all_suites_pass=all(r['exit']==0 for r in results)),indent=2)+'\n')
if not unchanged: raise SystemExit('driver changed')
