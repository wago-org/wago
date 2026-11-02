#!/usr/bin/env python3
"""Offline receipt audit only: never build, execute native artifacts, or dispatch."""
import collections, hashlib, io, json, pathlib, re, subprocess, tarfile, zipfile
P=pathlib.Path(__file__).resolve().parent
R=P.parents[2]
S='7e41fafbbcd737ed1edce11b94b43b4eda134dcf'
H='041f7ed4c376e2a33ce21b22075cd617ddca6597'
F='4913c91e1ad5636543d0ea7eed740b16de1b9bf2'
BASE='docs/rfwgc-evidence/darwin-tinygo-supplement/'
def sha(b):return hashlib.sha256(b).hexdigest()
def read(p):return json.loads(p.read_text())
def git(path,rev=S):return subprocess.check_output(['git','show',rev+':'+path],cwd=R)
def write(n,x):(P/n).write_text(json.dumps(x,indent=2,sort_keys=True)+'\n')
archive=subprocess.check_output(['git','archive',S],cwd=R)
with tarfile.open(fileobj=io.BytesIO(archive)) as t:
 source={m.name:sha(t.extractfile(m).read()) for m in t if m.isfile()}
plan=json.loads(git('docs/rfwgc-evidence/darwin-tinygo-fixture-repair/suite-plan.json',H))
prior=read(P.with_name('native-tinygo-run-41453552853')/'audit.json')
assert sha((P/'dispatch.json').read_bytes())=='0edd6aa9f33e354a951e20ec947806172512c9ba9a3af2f7a56c57fd2fb52b14'
assert len(read(P/'artifacts.json')['artifacts'])==3
jobs=read(P/'jobs.json')['jobs']
assert len(jobs)==29 and sum(j['conclusion']=='success' for j in jobs)==4
assert sum(j['conclusion']=='skipped' for j in jobs)==25
for j in jobs:
 assert j['status']=='completed' and j['run_id']==41466262594
 if j['conclusion']!='skipped':
  assert (P/('job-'+str(j['id'])+'.log')).read_text()==read(P/('job-'+str(j['id'])+'-tool.json'))['structuredContent']['content']
oldplan=json.loads(git(BASE+'suite-plan.json','8801252952255edf6ab6196250bd544873785328'))
for mode,suites in plan.items():
 for package,required in suites.items():
  extra=['TestDarwinTinyGoCancellationAndGC/'+n for n in ['cancel','close']] if package=='src/wago' else []
  assert set(required)==set(oldplan[mode][package]+extra)
  assert len(required)==len(set(required))
pin=json.loads(git(BASE+'toolchain.json',H))
# Independently rehash the preserved original compiler release without executing it.
release=R/BASE/'tinygo0.42.0.darwin-arm64.tar.gz'
assert sha(release.read_bytes())==pin['archive_sha256']
with tarfile.open(release) as t:
 assert sha(t.extractfile('tinygo/bin/tinygo').read())==pin['compiler_sha256']
 for name,h in pin['before'].items():assert sha(t.extractfile('tinygo/'+name).read())==h
# Reconstruct the four private runtime patch outputs in memory only.
with tarfile.open(release) as t:
 patched={n:t.extractfile('tinygo/'+n).read() for n in pin['before']}
replacements={
 'src/internal/task/task_threads.c':[(b'void tinygo_task_exit(void) {',b'void tinygo_task_exit_thread(void) {')],
 'src/internal/task/task_threads.go':[(b'//go:linkname tinygo_task_exit tinygo_task_exit\n',b'//go:linkname tinygo_task_exit tinygo_task_exit_thread\n'),(b'markRoots(t.state.stackBottom, t.state.stackTop)',b'WagoScanStack(t.state.stackBottom, t.state.stackTop)'),(b'\t// Scan all globals (implemented in the runtime).',b'\twagoScanForeignStacks()\n\n\t// Scan all globals (implemented in the runtime).')],
 'src/runtime/gc_stack_threads.go':[(b'markRoots(sp, task.StackTop())',b'task.WagoScanStack(sp, task.StackTop())')],
}
for name,changes in replacements.items():
 for old,new in changes:
  assert patched[name].count(old)==1;patched[name]=patched[name].replace(old,new)
patched['src/internal/task/wago_foreign_stack.go']=git('scripts/tinygo-042/wago_foreign_stack.go.in')
assert {n:sha(b) for n,b in patched.items()}==pin['after']
for name,h in pin['patch'].items():assert sha(git(name))==h
out={'source':S,'harness':H,'source_archive_sha256':sha(archive),'source_files':len(source),'release_sha256':pin['archive_sha256'],'compiler_sha256':pin['compiler_sha256'],'modes':{},'archives':[]}
for a in read(P/'artifacts.json')['artifacts']:
 mode=a['name'][14:-2]; raw=P/(mode+'.zip'); digest=sha(raw.read_bytes())
 assert 'sha256:'+digest==a['digest'];assert raw.stat().st_size==a['size_in_bytes']
 assert a['workflow_run']['id']==41466262594 and a['workflow_run']['head_sha']==H
 job=next(j for j in read(P/'jobs.json')['jobs'] if j['name']=='TinyGo Darwin ARM64 ('+mode+')')
 assert job['conclusion']=='success'
 upload=(P/('job-'+str(job['id'])+'.log')).read_text()
 assert 'SHA256 digest of uploaded artifact zip is '+digest in upload
 assert 'Final size is '+str(a['size_in_bytes'])+' bytes. Artifact ID is '+str(a['id']) in upload
 with zipfile.ZipFile(raw) as z:
  assert z.testzip() is None
  assert len(z.namelist())==41 and len(set(z.namelist()))==41
  # Materialize only original ZIP members; never execute any payload.
  for name in z.namelist():
   assert not pathlib.PurePosixPath(name).is_absolute() and '..' not in pathlib.PurePosixPath(name).parts
   path=P/'_artifacts'/mode/name
   if not path.exists():
    path.parent.mkdir(parents=True,exist_ok=True);path.write_bytes(z.read(name))
  for name in z.namelist():assert sha(z.read(name))==sha((P/'_artifacts'/mode/name).read_bytes())
 out['archives'].append({'mode':mode,'id':a['id'],'sha256':digest,'bytes':raw.stat().st_size,'files':len(z.namelist())})
 d=P/'_artifacts'/mode/('tinygo-supplement-'+mode)
 ident=read(d/'identity.json');assert ident['source']==S and ident['harness']==H
 assert ident['mode']==mode and ident['action']=='native'
 assert ident['source_archive_sha256']==sha(archive)
 assert ident['host']['system']=='Darwin' and ident['host']['machine']=='arm64'
 assert ident['toolchain']==pin and ident['suites']==plan[mode]
 assert read(d/'source-original.json')==source
 effective=dict(source)
 adapters=read(d/'adapters.json')
 assert set(adapters)=={'src/core/runtime/engine_int_tinygo_test.go','src/core/runtime/supplement_foreign_stack_arm64_test.go','src/wago/supplement_cancellation_test.go','src/wago/supplement_rejection_test.go','src/wago/supplement_stage8_test.go'}
 for name,a in adapters.items():
  if 'original' in a:
   original=git(a['original']);assert sha(original)==a['original_sha256']
   data=b'//go:build darwin && arm64 && tinygo\n'+original.split(b'\n',1)[1]
   if 'stage8' in name:
    option=b'.WithOptimization("dead-gc-new", false)'
    assert data.count(option)==2;data=data.replace(option,b'')
  elif name=='src/core/runtime/engine_int_tinygo_test.go':
   assert sha(git(name))==a['original_sha256']
   data=git(name,F)
  else:data=git(BASE+a['adapter'],H)
  inspected=P/'_inspected'/(name.replace('/','__')+'.effective.txt')
  inspected.parent.mkdir(exist_ok=True);inspected.write_bytes(data)
  assert sha(data)==a['sha256'];effective[name]=sha(data)
 assert read(d/'source-effective.json')==effective
 oldeffective=read(P.with_name('native-tinygo-run-41453552853')/'_artifacts'/mode/('tinygo-supplement-'+mode)/'source-effective.json')
 assert set(effective)==set(oldeffective)
 assert {n for n in effective if effective[n]!=oldeffective[n]}=={'src/core/runtime/engine_int_tinygo_test.go','src/wago/supplement_stage8_test.go','src/wago/supplement_cancellation_test.go'}
 assert all(effective[n]==h for n,h in source.items() if not n.endswith('_test.go'))
 assert read(d/'state.json')=={'mode':mode,'status':'PASS'}
 for name,h in ident['drivers'].items():assert sha(git(BASE+name,H))==h
 tools=read(d/'tool-effective.json')
 assert tools['bin/tinygo']==pin['compiler_sha256']
 for name,h in pin['after'].items():assert tools[name]==h
 assert read(d/'integrity.json')=={'unchanged':True}
 assert read(d/'host-tools.json')['go']=='go version go1.27.1 darwin/arm64'
 assert (P/'_artifacts'/mode/('tinygo-setup-'+mode)/'archive.sha256').read_text().split()[0]==pin['archive_sha256']
 for receipt in d.glob('*.exit.json'):
  e=read(receipt); log=receipt.with_name(receipt.name.replace('.exit.json','.log'));assert sha(log.read_bytes())==e['log_sha256']
 assert read(d/'patch.exit.json')['exit']==read(d/'patch-controls.exit.json')['exit']==0
 rows=[]
 for i,r in enumerate(read(d/'results.json')):
  package=r['package'];required=plan[mode][package];assert r['required']==required
  binary=(d/f'{i}.test').read_bytes();assert binary[:8]==bytes.fromhex('cffaedfe0c000001')
  assert sha(binary)==r['artifact_sha256']==read(d/f'{i}.artifact.json')['sha256']
  assert len(binary)==read(d/f'{i}.artifact.json')['size']
  log=(d/f'{i}-run.log').read_text()
  names={kind:re.findall(r'^\s*--- '+kind+r': (\S+)',log,re.M) for kind in ['PASS','FAIL','SKIP']}
  for kind,key in [('PASS','passed'),('FAIL','failed'),('SKIP','skipped')]:assert names[kind]==r['census'][key]
  assert read(d/f'{i}-build.exit.json')['exit']==r['build_exit']==0
  assert read(d/f'{i}-run.exit.json')['exit']==r['exit']
  build=read(d/f'{i}-build.command.json');run=read(d/f'{i}-run.command.json')
  assert '-scheduler=threads' in build['argv'] and '-gc=conservative' in build['argv']
  assert ('-tags='+mode in build['argv'])==(mode!='ordinary')
  assert build['argv'][-1]=='./'+package
  assert build['argv'][1:6]==['test','-c','-p=2','-scheduler=threads','-gc=conservative']
  assert build['timeout_seconds']==1200 and run['timeout_seconds']==600
  assert build['environment']==run['environment']
  for k,v in {'GOTOOLCHAIN':'local','GOENV':'off','GOWORK':'off','GOFLAGS':'-mod=readonly -p=2','GOMAXPROCS':'2'}.items():assert build['environment'][k]==v
  assert pathlib.PurePosixPath(run['cwd'])==pathlib.PurePosixPath(build['cwd'])/package
  pattern='^('+'|'.join(n for n in required if '/' not in n)+')$'
  assert run['argv'][1:]==['-test.v','-test.run='+pattern]
  assert build['argv'][build['argv'].index('-o')+1]==run['argv'][0]
  assert not names['FAIL'];assert not names['SKIP'];assert not r['census']['duplicates'];assert not r['census']['unexpected'];assert not r['census']['vacuous']
  assert not re.search('unavailable under TinyGo|depends on standard Go testing|no tests to run',log)
  assert all(c==1 for c in collections.Counter(names['PASS']).values())
  assert set(n for n in names['PASS'] if '/' not in n)==set(n for n in required if '/' not in n)
  ran=re.findall(r'^=== RUN\s+(\S+)',log,re.M)
  assert collections.Counter(ran)==collections.Counter(names['PASS'])
  missing=[n for n in required if names['PASS'].count(n)!=1];assert missing==r['census']['missing']==[]
  assert r['exit']==0 and r['census']['ok'] is True
  old=next(x for x in prior['modes'][mode]['suites'] if x['package']==package)
  assert all(names['PASS'].count(n)==1 for n in old['failed'])
  if package=='src/wago':
   assert all('TestDarwinTinyGoCancellationAndGC/'+n in required for n in ['cancel','close'])
  rows.append({'package':package,'build_exit':r['build_exit'],'run_exit':r['exit'],'sha256':sha(binary),'expected_top':sum('/' not in n for n in required),'passed_top':sum('/' not in n for n in names['PASS']),'failed':names['FAIL'],'skipped':names['SKIP'],'required_children':sum('/' in n for n in required),'passed_required_children':sum('/' in n and names['PASS'].count(n)==1 for n in required),'all_pass_children':sum('/' in n for n in names['PASS']),'missing_passes':missing,'build_command':build,'run_command':run,'verdict':'PASS' if r['exit']==0 and not missing else 'FAIL'})
 out['modes'][mode]={'expected_top':sum(x['expected_top'] for x in rows),'passed_top':sum(x['passed_top'] for x in rows),'suites':rows,'tool_files':len(tools),'source_effective_files':len(effective),'verdict':'PASS' if all(x['verdict']=='PASS' for x in rows) else 'FAIL'}
 assert out['modes'][mode]['passed_top']==out['modes'][mode]['expected_top']==(36 if mode=='wagodebug' else 33)
write('audit.json',out)
# Preserve exact inspected source/harness bytes as inert .txt files, not Go packages.
paths=['src/core/runtime/engine_int_tinygo_test.go','src/core/runtime/engine.go','src/core/runtime/invocation_other.go','src/core/runtime/trampoline_tinygo_arm64.go','src/wago/instance_call.go','src/wago/api.go','src/wago/config.go','src/wago/gc_stage8_tinygo_guest_amd64_test.go','src/core/compiler/optimization/catalog.go','src/wago/gc_conversion_store.go','scripts/patch-tinygo-foreign-stack.py','scripts/tinygo-042/wago_foreign_stack.go.in','tests/scripts/patch-tinygo-foreign-stack.py','scripts/patch-tinygo-042.sh']
(P/'_inspected').mkdir(exist_ok=True)
for path in paths:(P/'_inspected'/(path.replace('/','__')+'.txt')).write_bytes(git(path))
for name in ['harness.py','toolchain.json','cancellation_test.go.in','foreign_stack_arm64_test.go.in']:(P/'_inspected'/name).write_bytes(git(BASE+name,H))
(P/'_inspected'/'suite-plan.json').write_bytes(git('docs/rfwgc-evidence/darwin-tinygo-fixture-repair/suite-plan.json',H))
(P/'_inspected'/'activation-review.md').write_bytes(git('docs/rfwgc-evidence/darwin-tinygo-ready-recheck/README.md','c24f720181311d6a4b5d1b1eee9fd1caa29d0f28'))
(P/'_inspected'/'production-delta-to-harness.patch').write_bytes(subprocess.check_output(['git','diff',S,H,'--','src/wago/gc_conversion_store.go'],cwd=R))
write('inspected-source-manifest.json',{p.name:sha(p.read_bytes()) for p in (P/'_inspected').iterdir()})
print(json.dumps({m:{'expected':v['expected_top'],'passed':v['passed_top'],'verdict':v['verdict']} for m,v in out['modes'].items()},indent=2))
