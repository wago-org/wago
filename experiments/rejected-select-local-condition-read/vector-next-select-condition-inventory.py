from pathlib import Path
import hashlib,json,os,subprocess
work=Path('/tmp/wago-vector-next-37174169')
out=work/'experiments/arm64-parity/select-local-condition-read'
def run(args,tag):
 p=subprocess.run(args,cwd=work,capture_output=True,text=True,env=dict(os.environ,WAGO_SHARED_SCALAR='0',WAGO_BOUNDS='signals'))
 (out/(tag+'.raw')).write_text(p.stdout+p.stderr)
 if p.returncode:raise RuntimeError(p.stdout+p.stderr)
 print(tag,p.returncode,flush=True)
run(['go','build','-tags','wago_guardpage','-o','/tmp/inventory-select-local-condition-read','./experiments/arm64-parity/select-local-condition-read/inventory.go'],'inventory-build')
for on in [0,1]:run(['/tmp/inventory-select-local-condition-read',str(out/f'native-signal-{on}'),str(on)],f'inventory-{on}')
rows=[]
for kind in ['app','core']:
 base=Path('/tmp/hour-20261011-'+kind)
 files=list((out/'native-signal-0'/kind).glob('*.bin'))
 changed=[];drift=[]
 for p in files:
  a=p.read_bytes();b=(out/'native-signal-1'/kind/p.name).read_bytes()
  if a!=b:changed.append(p.name)
  if (base/p.name).read_bytes()!=a:drift.append(p.name)
  rows.append(dict(kind=kind,name=p.name,changed=a!=b,off_bytes=len(a),on_bytes=len(b),off_sha256=hashlib.sha256(a).hexdigest(),on_sha256=hashlib.sha256(b).hexdigest(),pristine_equal=(base/p.name).read_bytes()==a))
 print(kind,'count',len(files),'changed',len(changed),changed,'OFFdrift',drift,flush=True)
(out/'inventory.jsonl').write_text(''.join(json.dumps(x)+'\n' for x in rows))
