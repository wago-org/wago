#!/usr/bin/env python3
"""B-owned immutable combined qualification; no performance workloads."""
import hashlib,json,os,pathlib,shutil,subprocess,tarfile,tempfile
repo=pathlib.Path.cwd(); evidence=repo/'docs/rfwgc-evidence/r9-h2-host-imports/combined-3b04e6837'; evidence.mkdir(exist_ok=False)
pin='3b04e68376024c689ec651adf1817955ea75d707'
root=pathlib.Path(tempfile.mkdtemp(prefix='wago-h2-host-imports-committed-')); src=root/'source';src.mkdir()
sha=lambda p:hashlib.sha256(p.read_bytes()).hexdigest()
archive=root/'source.tar'
with archive.open('wb') as f:subprocess.run(['git','archive',pin],stdout=f,check=True)
with tarfile.open(archive) as f:f.extractall(src,filter='data')
for name in ['cache','tmp']:(root/name).mkdir()
goroot='/home/dev/.local/share/mise/installs/go/1.27.3'
env=dict(os.environ,GOROOT=goroot,GOTOOLCHAIN='local',GOWORK='off',GOCACHE=str(root/'cache'),GOTMPDIR=str(root/'tmp'))
env['PATH']=goroot+'/bin:/home/dev/Projects/wago/.tools/wabt-1.0.41-linux-x64/bin:'+env['PATH']
for k in ['WAGO_UPDATE_STAGED_SPEC','WAGO_SPEC_FILES','WAGO_SPECTEST_DIR','WAGO_SPEC_VERSION']:env.pop(k,None)
before={str(p.relative_to(src)):sha(p) for p in src.rglob('*') if p.is_file()}
(evidence/'provenance.json').write_text(json.dumps(dict(pin=pin,source=str(src),archive_sha256=sha(archive),go=subprocess.check_output([goroot+'/bin/go','version'],env=env,text=True).strip(),source_hashes={p:before[p] for p in json.loads((repo/'docs/rfwgc-evidence/r9-h2-host-imports/pending.json').read_text())['paths']}),indent=2)+'\n')
rows=[]
def run(label,args,cwd=src,expected=None):
 with (evidence/(label+'.log')).open('w') as f:
  try:code=subprocess.run(args,cwd=cwd,env=env,stdout=f,stderr=subprocess.STDOUT,timeout=1800).returncode
  except subprocess.TimeoutExpired:code=124
 log=(evidence/(label+'.log')).read_text();passes=[l for l in log.splitlines() if l.startswith('--- PASS:')];skips=[l for l in log.splitlines() if '--- SKIP:' in l];listed=[l for l in log.splitlines() if l.startswith('Test')]
 good=code==0
 if '-list' in args:good=good and bool(listed)
 elif args[0].endswith('/go'):good=good and bool(passes)
 if expected is not None:good=good and len(listed if '-list' in args else passes)==expected
 rows.append(dict(label=label,command=args,exit=code,passes=passes,skips=skips,listed=listed,accepted=good))
 (evidence/'results.json').write_text(json.dumps(rows,indent=2)+'\n');print(label,good,'pass',len(passes),'skip',len(skips),flush=True)
 if not good:raise RuntimeError(label)
go=goroot+'/bin/go';ok=False;failure=None
cells=['^TestR9H2StoreHooksOriginalProvider$','^TestR9H2HostContainerPublicGlobal','^TestR9H2HostImportGlobal','^TestR9H2StoreHooks(OriginalProvider|IndependentPhysicalTables)$','^TestR9H2HostContainerPublicTable','^TestR9H2HostImportTable','^TestR9H2StoreHooksBulk$','^TestR9H2HostImportBulk','^TestR9H2StoreHooksActiveInitialization$','^TestR9H2HostImportInitialization','^TestR9H2StoreHooksDescriptorMigration$','^TestR9H2HostContainerMigration','^TestR9H2Receipt(ConvertedProducerCycle|PublisherRetirement|CloseOrders|FinalJournalRemainsCollectionAuthority)$','^TestR9H2HostContainer(Cycle|Close)','^TestR9H2HostImportCycle','^TestR9H2Receipt(OnlyContention|IndependentContainersAndRollback|DomainKeepsPrivateTypeAdmission)$','^TestR9H2Host(Container|Import)Authority']
try:
 for mode in ['ordinary','wagodebug','wago_regalloccheck']:
  tags=[] if mode=='ordinary' else ['-tags',mode]
  for race in [False,True]:run(mode+'-nine-host-imports'+('-race' if race else ''),[go,'test',*tags,*(['-race'] if race else []),'./src/wago','-run','^TestR9H2HostImport','-count=1','-v','-timeout=3m'],expected=9)
 for i,rx in enumerate(cells):
  for mode in ['ordinary','wagodebug','wago_regalloccheck']:
   tags=[] if mode=='ordinary' else ['-tags',mode];label=f'cell-{i:02}-{mode}'
   run(label+'-list',[go,'test',*tags,'./src/wago','-list',rx])
   for race in [False,True]:run(label+('-race' if race else '-normal'),[go,'test',*tags,*(['-race'] if race else []),'./src/wago','-run',rx,'-count=1','-v','-timeout=3m'])
 for mode in ['ordinary','wagodebug','wago_regalloccheck']:
  tags=[] if mode=='ordinary' else ['-tags',mode]
  for arch in ['amd64','arm64']:
   pkg='./src/core/compiler/backend/railshot/'+arch
   run(mode+'-backend-'+arch+'-list',[go,'test',*tags,pkg,'-list','^TestExternHostStoreContract'],expected=3)
   run(mode+'-backend-'+arch,[go,'test',*tags,pkg,'-run','^TestExternHostStoreContract','-count=1','-v','-timeout=10m'],expected=3)
 for lane in ['integrated','targets','tinygo']:
  driver=src/('docs/rfwgc-evidence/r9-h2-production/qualify-'+lane+'.py')
  args=['python3',str(driver),'--pin',pin,'--out',str(evidence/lane)]
  if lane=='targets':
   assert sha(pathlib.Path('/tmp/qemu-user.deb'))=='ca6ede739327a20ae5a3498230c8a3a9a072c586e131bc6bcc1201eb1725e336';args+=['--qemu-package','/tmp/qemu-user.deb']
  run('driver-'+lane,args,cwd=repo)
  results=json.loads((evidence/lane/'results.json').read_text());assert len(results)=={'integrated':18,'targets':14,'tinygo':3}[lane] and all(r['exit']==0 for r in results)
 ok=True
except Exception as e:failure=str(e);print('STOP',failure,flush=True)
finally:
 changed=[p for p,h in before.items() if not (src/p).is_file() or sha(src/p)!=h]
 (evidence/'final.json').write_text(json.dumps(dict(pin=pin,all_pass=ok,host_acceptance=False,failure=failure,export_changes=changed,commands=len(rows),remaining_gates='All unexecuted cells/drivers remain required; no H2 closure.'),indent=2)+'\n')
 shutil.rmtree(root)
 (evidence/'cleanup.json').write_text(json.dumps(dict(removed=str(root)))+'\n')
raise SystemExit(0 if ok else 1)
