import pathlib,subprocess,tempfile,tarfile,json,os,hashlib,shutil
repo=pathlib.Path.cwd();out=repo/'docs/rfwgc-evidence/main-reconciliation-90f2/final-pin';out.mkdir(exist_ok=False)
pin='e65ff9373319e909d0f9f0035a960fef52f1e1d4'
work=pathlib.Path(tempfile.mkdtemp(prefix='_wago_rebase_final_'));src=work/'source';src.mkdir()
with (work/'source.tar').open('wb') as f:subprocess.run(['git','archive',pin,'--','.',':!docs'],stdout=f,check=True)
with tarfile.open(work/'source.tar') as t:t.extractall(src,filter='data')
go='/home/dev/.local/share/mise/installs/go/1.27.1/bin/go'
env=dict(os.environ,GOROOT=str(pathlib.Path(go).parent.parent),GOTOOLCHAIN='local',GOPROXY='off',GOWORK='off',WAGO_SHARED_SCALAR='0')
env['PATH']='/home/dev/Projects/wago/.tools/wabt-1.0.41-linux-x64/bin:/home/dev/.local/share/mise/installs/node/26.11.1/bin:'+str(pathlib.Path(go).parent)+':'+env['PATH']
rows=[]
for mode in ['ordinary','wagodebug','wago_regalloccheck']:
 for label,extra in [('focused-race',['-race','-run','^Test(Extern|GCLifetime|CallFree|LoopScan|InlineCaller)']),('diagnostic',[])]:
  tags=','.join(x for x in ['wago_codegenstats',mode if mode!='ordinary' else ''] if x)
  cmd=[go,'test','-tags='+tags,'./src/core/compiler/backend/railshot/amd64','./src/core/compiler/backend/railshot/arm64',*extra,'-count=1','-timeout=10m','-v']
  name=mode+'-'+label
  with (out/(name+'.log')).open('w') as f:r=subprocess.run(cmd,cwd=src,env=env,stdout=f,stderr=subprocess.STDOUT,timeout=720)
  rows.append(dict(label=name,argv=cmd,exit=r.returncode));(out/'results.json').write_text(json.dumps(rows,indent=2)+'\n');print(name,r.returncode,flush=True)
  if r.returncode:raise SystemExit(1)
(out/'provenance.json').write_text(json.dumps(dict(pin=pin,archive_sha256=hashlib.sha256((work/'source.tar').read_bytes()).hexdigest(),driver_sha256=hashlib.sha256(pathlib.Path(__file__).read_bytes()).hexdigest(),all_pass=True),indent=2)+'\n')
shutil.rmtree(work)
