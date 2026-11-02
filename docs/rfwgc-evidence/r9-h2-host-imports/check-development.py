#!/usr/bin/env python3
"""Immutable owned overlay: compilation + baseline only, never host acceptance."""
import hashlib,json,os,pathlib,shutil,subprocess,tempfile,sys
repo=pathlib.Path(__file__).resolve().parents[3]
evidence=pathlib.Path(__file__).resolve().parent
pin='67ef1f39e61dd61ee891bdefa5f8218a74aba806'
paths=['src/wago/gc_conversion_store.go','src/wago/gc_conversion_host_import_host_test.go','src/core/compiler/backend/railshot/amd64/extern_host_store_contract_test.go','src/core/compiler/backend/railshot/arm64/extern_host_store_contract_test.go']
root=pathlib.Path(tempfile.mkdtemp(prefix='wago-h2-host-imports-overlay-'))
source=root/'source';source.mkdir()
archive=root/'source.tar'
with archive.open('wb') as f: subprocess.run(['git','archive',pin],cwd=repo,stdout=f,check=True)
subprocess.run(['tar','-xf',str(archive),'-C',str(source)],check=True)
for p in paths: shutil.copy2(repo/p,source/p)
sha=lambda p:hashlib.sha256(p.read_bytes()).hexdigest()
before={p:sha(source/p) for p in paths}
logs=evidence/root.name;logs.mkdir()
(logs/'provenance.json').write_text(json.dumps(dict(pin=pin,source=str(source),archive_sha256=sha(archive),overlay=before,scope='development; no host acceptance'),indent=2)+'\n')
(root/'cache').mkdir();(root/'tmp').mkdir()
env=os.environ.copy();goroot='/home/dev/.local/share/mise/installs/go/1.27.3'
env.update(GOROOT=goroot,GOTOOLCHAIN='local',GOWORK='off',GOCACHE=str(root/'cache'),GOTMPDIR=str(root/'tmp'),PATH=goroot+'/bin:/home/dev/Projects/wago/.tools/wabt-1.0.41-linux-x64/bin:'+env['PATH'])
rows=[]
def run(label,args,expected=None):
 p=subprocess.run(['go','test']+args,cwd=source,env=env,text=True,stdout=subprocess.PIPE,stderr=subprocess.STDOUT)
 (logs/(label+'.log')).write_text(p.stdout)
 passes=[l for l in p.stdout.splitlines() if l.startswith('--- PASS:')]
 skips=[l for l in p.stdout.splitlines() if '--- SKIP:' in l]
 listed=[l for l in p.stdout.splitlines() if l.startswith('Test')]
 count=len(listed) if '-list' in args else len(passes)
 good=p.returncode==0 and (expected is None or count==expected) and not skips
 rows.append(dict(label=label,args=args,exit=p.returncode,passed=len(passes),listed=listed,skips=skips,expected=expected,accepted=good))
 (logs/'results.json').write_text(json.dumps(rows,indent=2)+'\n')
 print(label,good,'count',count,flush=True)
 if not good: print(p.stdout,flush=True)
 return good
ok=True
for mode in ['ordinary','wagodebug','wago_regalloccheck']:
 tags=[] if mode=='ordinary' else ['-tags',mode]
 ok=run(mode+'-host-census',tags+['./src/wago','-list','^TestR9H2HostImport'],9) and ok
 if '--census-only' in sys.argv: continue
 for race in [False,True]:
  ok=run(mode+('-race' if race else '-baseline'),tags+(['-race'] if race else [])+['./src/wago','-run','^TestR9H2StoreHooks','-count=1','-v','-timeout=3m'],5) and ok
 for arch in ['amd64','arm64']:
  ok=run(mode+'-backend-'+arch,tags+['./src/core/compiler/backend/railshot/'+arch,'-run','^TestExternHostStoreContract','-count=1','-v','-timeout=10m'],3) and ok
unchanged=before=={p:sha(source/p) for p in paths}
(logs/'final.json').write_text(json.dumps(dict(all_pass=ok,overlay_unchanged=unchanged,host_acceptance=False),indent=2)+'\n')
print('OUTPUT',logs,flush=True)
raise SystemExit(0 if ok and unchanged else 1)
