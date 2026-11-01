#!/usr/bin/env python3
"""Immutable correctness export; no timing or benchmark collection."""
import hashlib, json, os, pathlib, subprocess, sys, tempfile
repo=pathlib.Path(__file__).resolve().parents[3]
head=subprocess.check_output(['git','rev-parse',sys.argv[1]],cwd=repo,text=True).strip()
out=pathlib.Path(sys.argv[2]).resolve(); out.mkdir(parents=True,exist_ok=False)
source=pathlib.Path(tempfile.mkdtemp(prefix='wago-stage8-source-'))
archive=subprocess.check_output(['git','archive',head],cwd=repo)
subprocess.run(['tar','-x','-C',str(source)],input=archive,check=True)
env=os.environ.copy(); env.update(GOROOT='/home/dev/.local/share/mise/installs/go/1.27.1', GOTOOLCHAIN='local', GOWORK='off', GOMAXPROCS='2', GOFLAGS='-p=2')
env['PATH']='/home/dev/.local/share/wago-tinygo-investigation/tinygo/bin:'+env['GOROOT']+'/bin:'+env['PATH']
env['XDG_CACHE_HOME']=tempfile.mkdtemp(prefix='wago-stage8-cache-'); env['GOCACHE']=env['XDG_CACHE_HOME']+'/go-build'
driver=pathlib.Path(__file__).read_bytes(); (out/'driver.py').write_bytes(driver)
provenance={'head':head,'source':str(source),'cache':env['XDG_CACHE_HOME'],'driver_sha256':hashlib.sha256(driver).hexdigest(),'archive_sha256':hashlib.sha256(archive).hexdigest(),'env':{k:env[k] for k in ['GOROOT','PATH','GOTOOLCHAIN','GOWORK','GOMAXPROCS','GOFLAGS','XDG_CACHE_HOME','GOCACHE']}}
(out/'source.json').write_text(json.dumps(provenance,indent=2)+'\n')
results=[]
def run(label,cmd):
 (out/(label+'.command.json')).write_text(json.dumps(cmd)+'\n')
 with (out/(label+'.log')).open('w') as log:
  try: code=subprocess.run(cmd,cwd=source,env=env,stdout=log,stderr=subprocess.STDOUT,timeout=1800).returncode
  except subprocess.TimeoutExpired: code=124
 results.append({'label':label,'command':cmd,'exit':code}); (out/'results.json').write_text(json.dumps(results,indent=2)+'\n'); print(label,code,flush=True)
run('compiler',['tinygo','version'])
run('bootstrap',['bash','scripts/patch-tinygo-042.sh'])
for mode in ['ordinary','wagodebug','wago_regalloccheck']:
 tags=[] if mode=='ordinary' else ['-tags='+mode]
 run('go-'+mode,['go','test','-count=1','-timeout=25m',*tags,'-run','Evacuation','./src/core/runtime/gc/native'])
 run('tinygo-'+mode,['tinygo','test','-scheduler=threads','-gc=conservative',*tags,'-v','-run','Evacuation','./src/core/runtime/gc/native'])
assert driver==pathlib.Path(__file__).read_bytes(), 'driver mutated during run'
(out/'driver-unchanged.txt').write_text('PASS\n')
sys.exit(any(x['exit'] for x in results))
