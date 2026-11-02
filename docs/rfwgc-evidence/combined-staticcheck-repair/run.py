#!/usr/bin/env python3
"""Correctness gates on immutable exports; no benchmark or measurement commands."""
import pathlib,subprocess,os,json,hashlib,sys
E=pathlib.Path(__file__).resolve().parent
W=pathlib.Path((E/'workspace.txt').read_text().strip())
G='/home/dev/.local/share/mise/installs/go/1.27.3'
Q='/tmp/qemu-x/usr/bin'
phase=sys.argv[1]; S=W/phase
out=E/phase;out.mkdir(exist_ok=True)
def sha(p):return hashlib.sha256(pathlib.Path(p).read_bytes()).hexdigest()
env=dict(os.environ,GOROOT=G,GOTOOLCHAIN='local',GOMAXPROCS='2',GOFLAGS='-p=2',PATH=str(S/'.tools/staticcheck')+':'+G+'/bin:/usr/bin:/bin')
def run(label,cmd,extra=None,cwd=None):
 d=out/label;d.mkdir(exist_ok=False);ee=dict(env,**(extra or {}))
 (d/'command.json').write_text(json.dumps(dict(argv=cmd,cwd=str(cwd or S),environment={k:ee.get(k) for k in ['GOROOT','GOTOOLCHAIN','GOMAXPROCS','GOFLAGS','PATH','GOOS','GOARCH','CGO_ENABLED']},driver_sha256=sha(__file__)),indent=2)+'\n')
 with (d/'raw.log').open('w') as f:
  try: code=subprocess.run(cmd,cwd=cwd or S,env=ee,stdout=f,stderr=subprocess.STDOUT,timeout=1800).returncode
  except subprocess.TimeoutExpired:code=124
 (d/'result.json').write_text(json.dumps({'exit':code})+'\n');print(phase,label,code,flush=True);return code
if phase=='before':
 run('bootstrap',['/bin/sh','scripts/bootstrap-staticcheck.sh','--verify'])
 run('staticcheck',[G+'/bin/go','run','./tests/tools/staticcheck-baseline','verify'])
 for arch,q in [('amd64',None),('riscv64','qemu-riscv64')]:
  binary=W/('before-'+arch+'.test');ee={'GOOS':'linux','GOARCH':arch,'CGO_ENABLED':'0'}
  if run(arch+'-build',[G+'/bin/go','test','-c','-o',str(binary),'./src/wago'],ee)==0:
   run(arch+'-shape',([Q+'/'+q] if q else [])+[str(binary),'-test.v','-test.run=^TestStaticcheckShapeProbe$'],ee,S/'src/wago')
if phase=='before-lint':
 S=W/'before'
 env.update(GOROOT='/home/dev/.local/share/mise/installs/go/1.27.1',PATH=str(S/'.tools/staticcheck')+':/home/dev/.local/share/mise/installs/go/1.27.1/bin:/usr/bin:/bin')
 run('bootstrap',['/bin/sh','scripts/bootstrap-staticcheck.sh','--verify'])
 run('staticcheck',['/home/dev/.local/share/mise/installs/go/1.27.1/bin/go','run','./tests/tools/staticcheck-baseline','verify'])
