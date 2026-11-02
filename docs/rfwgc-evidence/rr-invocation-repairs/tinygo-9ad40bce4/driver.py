#!/usr/bin/env python3
import argparse,hashlib,json,os,pathlib,subprocess,tarfile,tempfile
p=argparse.ArgumentParser();p.add_argument('--pin',required=True);p.add_argument('--out',required=True);a=p.parse_args()
out=pathlib.Path(a.out).resolve();out.mkdir(parents=True,exist_ok=False);src=pathlib.Path(tempfile.mkdtemp(prefix='wago-h2-tinygo-'))
pin=subprocess.check_output(['git','rev-parse',a.pin],text=True).strip()
archive=src/'source.tar'
with archive.open('wb') as f:subprocess.run(['git','archive',pin,'--','.',':!docs'],stdout=f,check=True)
with tarfile.open(archive) as tf:tf.extractall(src,filter='data')
def sha(p):return hashlib.sha256(pathlib.Path(p).read_bytes()).hexdigest()
tinygo='/home/dev/.local/share/wago-tinygo-investigation/tinygo/bin/tinygo'
go='/home/dev/.local/share/mise/installs/go/1.27.1'
env=dict(os.environ,GOROOT=go,GOTOOLCHAIN='local',GOWORK='off',GOMAXPROCS='2',GOFLAGS='-p=2')
env['PATH']=str(pathlib.Path(tinygo).parent)+':'+go+'/bin:/home/dev/Projects/wago/.tools/wabt-1.0.41-linux-x64/bin:/home/dev/.local/share/mise/installs/github-bytecodealliance-wasm-tools/1.251.0:'+env['PATH']
files={str(f.relative_to(src)):sha(f) for f in src.rglob('*') if f.is_file()}
driver=sha(__file__);(out/'driver.py').write_bytes(pathlib.Path(__file__).read_bytes())
(out/'provenance.json').write_text(json.dumps(dict(pin=pin,source=str(src),archive_sha256=sha(archive),driver_sha256=driver,tinygo_sha256=sha(tinygo),version=subprocess.check_output([tinygo,'version'],env=env,text=True).strip()),indent=2)+'\n')
rows=[]
for mode in ['ordinary','wagodebug','wago_regalloccheck']:
 tags='wago_runtime'+('' if mode=='ordinary' else ','+mode)
 cmd=[tinygo,'test','-scheduler=threads','-gc=conservative','-tags='+tags,'-run=^TestGCExternConversion','-v','./src/wago']
 (out/(mode+'.command.json')).write_text(json.dumps(cmd)+'\n')
 with (out/(mode+'.log')).open('w') as f:
  try:code=subprocess.run(cmd,cwd=src,env=env,stdout=f,stderr=subprocess.STDOUT,timeout=900).returncode
  except subprocess.TimeoutExpired:code=124
 text=(out/(mode+'.log')).read_text();rows.append(dict(mode=mode,exit=code,passes=[x for x in text.splitlines() if '--- PASS:' in x],skips=[x for x in text.splitlines() if '--- SKIP:' in x],failures=[x for x in text.splitlines() if '--- FAIL:' in x]))
 (out/'results.json').write_text(json.dumps(rows,indent=2)+'\n');print(mode,code,flush=True)
 if code:break
(out/'driver-final.json').write_text(json.dumps(dict(driver_unchanged=sha(__file__)==driver,export_changes=[rel for rel,h in files.items() if not (src/rel).is_file() or sha(src/rel)!=h],all_pass=len(rows)==3 and all(r['exit']==0 for r in rows)),indent=2)+'\n')
