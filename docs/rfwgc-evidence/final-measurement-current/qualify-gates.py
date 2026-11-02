#!/usr/bin/env python3
"""Additional correctness gates only, on retained executables; no clocks or benches."""
import pathlib,json,subprocess,hashlib,re,os,signal
H=pathlib.Path(__file__).resolve().parent;O=H/'.prepared';V=H/'gates';V.mkdir();rows=[]
bindings=H/'qualification-guest-repair/builds.json'
if not bindings.exists():bindings=H/'qualification/builds.json'
b=json.loads(bindings.read_text())
for pin in ['baseline','port']:
 for mode in ['ordinary','wagodebug','wago_regalloccheck']:
  binding=b['native-linux-amd64/'+pin+'/guest'][mode]
  for selector in ['TestCurrentPreparationDesignReviewExternStorageImportsPrivateGCProducer','TestCurrentPreparationDesignReviewExternStorageProducerCrossRuntimeImport']:
   binary=pathlib.Path(binding['binary']);assert hashlib.sha256(binary.read_bytes()).hexdigest()==binding['sha256']
   cmd=[str(binary),'-test.run=^'+selector+'$','-test.v','-test.count=1','-test.timeout=5m'];log=V/f'{pin}-{mode}-{selector}.log';cwd=O/pin/'src/wago';env=binding['build']['env']
   with log.open('w') as f:
    proc=subprocess.Popen(cmd,cwd=cwd,env=env,stdout=f,stderr=subprocess.STDOUT,start_new_session=True)
    try:code=proc.wait(timeout=360)
    except subprocess.TimeoutExpired:os.killpg(proc.pid,signal.SIGKILL);proc.wait();code='TIMEOUT'
   raw=log.read_text();passes=re.findall(r'^\s*--- PASS: (\S+)',raw,re.M);skips=re.findall(r'^\s*--- SKIP: (\S+)',raw,re.M)
   rows.append(dict(pin=pin,mode=mode,argv=cmd,cwd=str(cwd),env=env,exit=code,binary_sha256=binding['sha256'],log=str(log.relative_to(H)),log_sha256=hashlib.sha256(log.read_bytes()).hexdigest(),passes=passes,skips=skips,status='PASS' if code==0 and passes and not skips else 'FAILED_OR_EMPTY'))
   (V/'receipts.json').write_text(json.dumps(rows,indent=2)+'\n');print(pin,mode,selector,rows[-1]['status'],flush=True)
