import pathlib,json,hashlib
E=pathlib.Path(__file__).resolve().parent
p=E/'run.py';s=p.read_text().replace("GOWORK='off',",'')
s=s.replace("import os,sys,json,pathlib,subprocess,hashlib,gzip,signal","import os,sys,json,pathlib,subprocess,hashlib,gzip,signal,datetime")
s=s.replace("out=E/name;out.mkdir(exist_ok=False); ev=env| (extra or {})", "assert not (E/'SAFETY-STOP.json').exists(), 'unsafe invariant: execution stopped'\n out=E/name;out.mkdir(exist_ok=False); ev=env| (extra or {})")
s=s.replace("with (out/'raw.log').open('w') as f:\n  p=subprocess.run(cmd,cwd=cwd,env=ev,stdout=f,stderr=subprocess.STDOUT)",'''with (out/'raw.log').open('w') as f:
  p=subprocess.Popen(cmd,cwd=cwd,env=ev,stdout=subprocess.PIPE,stderr=subprocess.STDOUT,text=True,start_new_session=True)
  (out/'process.json').write_text(json.dumps({'pid':p.pid,'started_utc':datetime.datetime.now(datetime.timezone.utc).isoformat()}))
  for line in p.stdout:
   f.write(line);f.flush()
   # Compiler rejection and detected data races prohibit any later qualification execution.
   if any(x in line for x in ['WARNING: DATA RACE','regalloccheck: function','fatal error:','internal compiler error in function','SIGSEGV: segmentation violation']):
    (E/'SAFETY-STOP.json').write_text(json.dumps({'source':M['pin'],'command':cmd,'gate':name,'line':line,'observed_utc':datetime.datetime.now(datetime.timezone.utc).isoformat()},indent=2))
    os.killpg(p.pid,signal.SIGTERM)
  p.wait()''')
s=s.replace("'driver_unchanged':D==", "'raw_sha256':hashlib.sha256((out/'raw.log').read_bytes()).hexdigest(),'driver_unchanged':D==")
p.write_text(s)
p=E/'corpus-summary.py';p.write_text(p.read_text().replace('501e9ab46c4f68090c7bbeb17a6ad14f4fadda6d','5345bd365c13963f1bf60e26bb80789ab9ead4cc'))
