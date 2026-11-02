#!/usr/bin/env python3
"""Execute each adopted matrix cell, with a nonzero exact test census."""
import argparse,hashlib,json,os,pathlib,subprocess
p=argparse.ArgumentParser();p.add_argument('--source',required=True);p.add_argument('--out',required=True);a=p.parse_args()
src=pathlib.Path(a.source);out=pathlib.Path(a.out);out.mkdir(parents=True,exist_ok=False)
go='/home/dev/.local/share/mise/installs/go/1.27.3/bin/go'
env=dict(os.environ,GOROOT=str(pathlib.Path(go).parent.parent),GOTOOLCHAIN='local',GOWORK='off')
env['PATH']='/home/dev/Projects/wago/.tools/wabt-1.0.41-linux-x64/bin:'+str(pathlib.Path(go).parent)+':'+env['PATH']
cells=[('^TestR9H2StoreHooksOriginalProvider$',1),('^TestR9H2StoreHooks(OriginalProvider|IndependentPhysicalTables)$',2),('^TestR9H2StoreHooksBulk$',1),('^TestR9H2StoreHooksActiveInitialization$',1),('^TestR9H2StoreHooksDescriptorMigration$',1),('^TestR9H2Receipt(ConvertedProducerCycle|PublisherRetirement|CloseOrders|FinalJournalRemainsCollectionAuthority)$',4),('^TestR9H2Receipt(OnlyContention|IndependentContainersAndRollback|DomainKeepsPrivateTypeAdmission)$',3),('^TestR9H2HostContainerPublicGlobal',1),('^TestR9H2HostContainerPublicTable',1),('^TestR9H2HostContainerMigration',1),('^TestR9H2HostContainer(Cycle|Close)',2),('^TestR9H2Host(Container|Import)Authority',1)]
rows=[]
for mode in ['ordinary','wagodebug','wago_regalloccheck']:
 tags=[] if mode=='ordinary' else ['-tags',mode]
 for i,(rx,count) in enumerate(cells):
  cmd=[go,'test',*tags,'./src/wago','-list',rx];r=subprocess.run(cmd,cwd=src,env=env,text=True,capture_output=True)
  names=[line for line in r.stdout.splitlines() if line.startswith('Test')]
  assert r.returncode==0 and len(names)==count,(mode,rx,count,names,r.stderr)
  for race in [False,True]:
   label=f'{mode}-{i:02d}'+('-race' if race else '')
   cmd=[go,'test',*tags,*(['-race'] if race else []),'./src/wago','-run',rx,'-count=1','-v','-timeout=3m']
   with (out/(label+'.log')).open('w') as f:r=subprocess.run(cmd,cwd=src,env=env,stdout=f,stderr=subprocess.STDOUT)
   log=(out/(label+'.log')).read_text();passed=[line.split()[2] for line in log.splitlines() if line.startswith('--- PASS:')];skips=[line for line in log.splitlines() if '--- SKIP:' in line]
   rows.append(dict(label=label,command=cmd,exit=r.returncode,expected=names,passed=passed,skips=skips))
   (out/'results.json').write_text(json.dumps(rows,indent=2)+'\n');print(label,r.returncode,flush=True)
   assert r.returncode==0 and sorted(passed)==sorted(names) and not skips,(label,passed,names,skips)
(out/'final.json').write_text(json.dumps(dict(commands=len(rows),all_pass=True,source=str(src),driver_sha256=hashlib.sha256(pathlib.Path(__file__).read_bytes()).hexdigest()),indent=2)+'\n')
