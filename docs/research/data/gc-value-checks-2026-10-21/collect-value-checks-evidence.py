from pathlib import Path
import hashlib,json,shutil,gzip,subprocess
r=Path('/home/dev/Projects/dewdrop');raw=r/'.tmp/wago-next-gc-perf/value-checks';e=r/'docs/research/data/wago-value-checks-2026-10-21';e.mkdir(parents=True,exist_ok=True)
for name in ['full-tests-combined','final-package-tests','final-public-packages','full-tests-public','full-tests-public-before-direct-child','final-package-tests-before-direct-child']:
 if (raw/name).exists():shutil.copytree(raw/name,e/name,dirs_exist_ok=True)
for p in raw.glob('*'):
 if p.is_file() and p.suffix in ['.log','.json']:shutil.copyfile(p,e/p.name)
for name in ['micro','micro-final','admission']:
 dst=e/name;dst.mkdir(exist_ok=True)
 if not (raw/name).exists():continue
 for p in (raw/name).iterdir():
  if p.is_file() and p.suffix in ['.json','.log','.py','.txt']:
   if p.suffix=='.json' and p.stat().st_size>200000:
    with gzip.open(dst/(p.name+'.gz'),'wb') as f:f.write(p.read_bytes())
   else:shutil.copyfile(p,dst/p.name)
for name in ['prepare-value-checks-full.py','measure-value-checks-full.py','prepare-value-checks-final-full.py','measure-value-checks-final-full.py','qualify-value-checks-combined.py','qualify-value-checks-public.py','finish-value-checks-tests.py','finish-value-checks-admission.py','install-value-checks.py','collect-value-checks-evidence.py','prepare-value-checks-micro.py','measure-value-checks-micro.py','prepare-value-checks-final-micro.py','measure-value-checks-final-micro.py']:
 shutil.copyfile(r/'.tmp/wago-next-gc-perf'/name,e/name)
review=e/'review-round-1';review.mkdir(exist_ok=True)
for name in ['review.md','mode-evidence.json','request.json','result.json']:
 shutil.copyfile(r/'.tmp/wago-next-gc-perf/daybreak-value-checks'/name,review/name)
for src,dst in [('wago-value-checks-full-2026-10-21','initial-full'),('wago-value-checks-final-full-2026-10-21','final-full')]:
 source=r/'docs/research/data'/src
 if source.exists():shutil.copytree(source,e/dst,dirs_exist_ok=True)
files=subprocess.check_output(['git','diff','--name-only','--','src'],cwd=r/'.tmp/wago-gc-value-checks',text=True).splitlines();files+=['src/core/compiler/backend/railshot/amd64/gc_value_checks.go']
identity={}
for label,tree in [('combined',r/'.tmp/wago-gc-value-checks'),('public',r/'.tmp/wago-gc-read-regions-public')]:
 identity[label]={'base_commit':subprocess.check_output(['git','rev-parse','HEAD'],cwd=tree,text=True).strip(),'files':{p:hashlib.sha256((tree/p).read_bytes()).hexdigest() for p in files}}
identity['arm64_edit_sha256']=hashlib.sha256((r/'.tmp/wago-local-stack-2026-10-20/wago/src/core/compiler/backend/railshot/arm64/call.go').read_bytes()).hexdigest()
(e/'final-source-identity.json').write_text(json.dumps(identity,indent=2)+'\n')
# Keep exact raw logs compressed, with whitespace-clean readable copies.
for p in e.rglob('*.log'):
 raw=p.read_bytes()
 clean=b'\n'.join(line.rstrip() for line in raw.split(b'\n'))
 if clean!=raw:
  with gzip.open(str(p)+'.gz','wb') as f:f.write(raw)
  p.write_bytes(clean)
print(json.dumps({'evidence':str(e),'bytes':sum(p.stat().st_size for p in e.rglob('*') if p.is_file()),'files':len(list(e.rglob('*')))}))
