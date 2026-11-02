#!/usr/bin/env python3
"""Immutable correctness gates; no performance workloads."""
import argparse,hashlib,json,os,pathlib,subprocess,tarfile,tempfile
p=argparse.ArgumentParser();p.add_argument('--pin',required=True);p.add_argument('--out',required=True);a=p.parse_args()
repo=pathlib.Path.cwd();out=pathlib.Path(a.out).resolve();out.mkdir(parents=True,exist_ok=False)
work=pathlib.Path(tempfile.mkdtemp(prefix='wago-h2-integrated-'));src=work/'source';src.mkdir()
def sha(p):return hashlib.sha256(pathlib.Path(p).read_bytes()).hexdigest()
pin=subprocess.check_output(['git','rev-parse',a.pin],text=True).strip()
with (work/'source.tar').open('wb') as f:subprocess.run(['git','archive',pin],stdout=f,check=True)
with tarfile.open(work/'source.tar') as tf:tf.extractall(src,filter='data')
for v in [1,2,3]:
 rel='tests/conformance/spec-v'+str(v);cpin=subprocess.check_output(['git','ls-tree',pin,rel],text=True).split()[2]
 with (work/('corpus'+str(v)+'.tar')).open('wb') as f:subprocess.run(['git','-C',str(repo/rel),'archive',cpin],stdout=f,check=True)
 with tarfile.open(work/('corpus'+str(v)+'.tar')) as tf:tf.extractall(src/rel,filter='data')
go='/home/dev/.local/share/mise/installs/go/1.27.3/bin/go'
env=dict(os.environ,GOROOT=str(pathlib.Path(go).parent.parent),GOTOOLCHAIN='local',GOWORK='off',WAGO_SPEC_INTERPRETER='/home/dev/Projects/wago/.tools/spec-interpreter-9d36019973201a19f9c9ebb0f10828b2fe2374aa/wasm',WAGO_SPEC_INTERPRETER_REVISION='9d36019973201a19f9c9ebb0f10828b2fe2374aa',WAGO_WAST2JSON='/home/dev/Projects/wago/.tools/wabt-1.0.41-linux-x64/bin/wast2json',WAGO_WABT_VERSION='1.0.41')
env['PATH']='/home/dev/Projects/wago/.tools/wabt-1.0.41-linux-x64/bin:/home/dev/.local/share/mise/installs/node/26.11.1/bin:'+str(pathlib.Path(go).parent)+':'+env['PATH']
for k in ['WAGO_UPDATE_STAGED_SPEC','WAGO_SPEC_FILES','WAGO_SPECTEST_DIR','WAGO_SPEC_VERSION']:env.pop(k,None)
files={str(f.relative_to(src)):sha(f) for f in src.rglob('*') if f.is_file()}
(out/'driver.py').write_bytes(pathlib.Path(__file__).read_bytes());driverhash=sha(__file__)
(out/'provenance.json').write_text(json.dumps(dict(pin=pin,source=str(src),archive_sha256=sha(work/'source.tar'),driver_sha256=driverhash,go=subprocess.check_output([go,'version'],env=env,text=True).strip(),corpus=subprocess.check_output(['git','ls-tree',pin,'tests/conformance/spec-v1','tests/conformance/spec-v2','tests/conformance/spec-v3'],text=True)),indent=2)+'\n')
rows=[]
def run(label,cmd,e=env):
 (out/(label+'.command.json')).write_text(json.dumps(cmd)+'\n')
 with (out/(label+'.log')).open('w') as f:
  try:code=subprocess.run(cmd,cwd=src,env=e,stdout=f,stderr=subprocess.STDOUT,timeout=1500).returncode
  except subprocess.TimeoutExpired:code=124
 log=(out/(label+'.log')).read_text();rows.append(dict(label=label,exit=code,skip=[x for x in log.splitlines() if '--- SKIP:' in x],fail=[x for x in log.splitlines() if '--- FAIL:' in x]))
 (out/'results.json').write_text(json.dumps(rows,indent=2)+'\n');print(label,code,flush=True)
 return code==0
ok=True
for mode in ['ordinary','wagodebug','wago_regalloccheck']:
 tags=[] if mode=='ordinary' else ['-tags',mode]
 for label,extra in [('focused',['./src/wago','-run','^Test(R9(H2|Ownership)|GCExternConversion)','-timeout=3m']),('full',['./src/wago','-timeout=20m']),('race',['-race','./src/wago','-run','^Test(R9(H2|Ownership)|GCExternConversion)','-timeout=3m']),('backend',['./src/core/compiler/backend/railshot/amd64','./src/core/compiler/backend/railshot/arm64','./src/core/compiler/wasm','-timeout=10m'])]:
  if not run(mode+'-'+label,[go,'test',*tags,*extra,'-count=1','-v']):ok=False;break
 if not ok:break
 for arch in ['arm64','riscv64']:
  if not run(mode+'-cross-'+arch,[go,'test',*tags,'./src/wago','-run','^$','-exec=true'],dict(env,GOOS='linux',GOARCH=arch)):ok=False;break
 if not ok:break
changed=[rel for rel,h in files.items() if not (src/rel).is_file() or sha(src/rel)!=h]
(out/'driver-final.json').write_text(json.dumps(dict(driver_unchanged=sha(__file__)==driverhash,export_changes=changed,all_pass=ok,commands=len(rows)),indent=2)+'\n')
raise SystemExit(0 if ok and not changed else 1)
