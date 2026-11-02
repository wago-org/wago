#!/usr/bin/env python3
"""Offline receipt audit only: never build, execute native artifacts, or dispatch."""
import collections, hashlib, io, json, pathlib, re, subprocess, tarfile, zipfile
P=pathlib.Path(__file__).resolve().parent
R=P.parents[2]
S='7e41fafbbcd737ed1edce11b94b43b4eda134dcf'
H='8801252952255edf6ab6196250bd544873785328'
BASE='docs/rfwgc-evidence/darwin-tinygo-supplement/'
def sha(b):return hashlib.sha256(b).hexdigest()
def read(p):return json.loads(p.read_text())
def git(path,rev=S):return subprocess.check_output(['git','show',rev+':'+path],cwd=R)
def write(n,x):(P/n).write_text(json.dumps(x,indent=2,sort_keys=True)+'\n')
archive=subprocess.check_output(['git','archive',S],cwd=R)
with tarfile.open(fileobj=io.BytesIO(archive)) as t:
 source={m.name:sha(t.extractfile(m).read()) for m in t if m.isfile()}
plan=json.loads(git(BASE+'suite-plan.json',H))
pin=json.loads(git(BASE+'toolchain.json',H))
# Independently rehash the preserved original compiler release without executing it.
release=R/BASE/'tinygo0.42.0.darwin-arm64.tar.gz'
assert sha(release.read_bytes())==pin['archive_sha256']
with tarfile.open(release) as t:
 assert sha(t.extractfile('tinygo/bin/tinygo').read())==pin['compiler_sha256']
 for name,h in pin['before'].items():assert sha(t.extractfile('tinygo/'+name).read())==h
for name,h in pin['patch'].items():assert sha(git(name))==h
out={'source':S,'harness':H,'source_archive_sha256':sha(archive),'source_files':len(source),'release_sha256':pin['archive_sha256'],'compiler_sha256':pin['compiler_sha256'],'modes':{},'archives':[]}
for a in read(P/'artifacts.json')['artifacts']:
 mode=a['name'][14:-2]; raw=P/(mode+'.zip'); digest=sha(raw.read_bytes())
 assert 'sha256:'+digest==a['digest'];assert raw.stat().st_size==a['size_in_bytes']
 with zipfile.ZipFile(raw) as z:
  assert z.testzip() is None
  for name in z.namelist():assert sha(z.read(name))==sha((P/'_artifacts'/mode/name).read_bytes())
 out['archives'].append({'mode':mode,'id':a['id'],'sha256':digest,'bytes':raw.stat().st_size,'files':len(z.namelist())})
 d=P/'_artifacts'/mode/('tinygo-supplement-'+mode)
 ident=read(d/'identity.json');assert ident['source']==S and ident['harness']==H
 assert ident['source_archive_sha256']==sha(archive)
 assert ident['host']['system']=='Darwin' and ident['host']['machine']=='arm64'
 assert ident['toolchain']==pin and ident['suites']==plan[mode]
 assert read(d/'source-original.json')==source
 effective=dict(source)
 for name,a in read(d/'adapters.json').items():
  if 'original' in a:
   original=git(a['original']);assert sha(original)==a['original_sha256']
   data=b'//go:build darwin && arm64 && tinygo\n'+original.split(b'\n',1)[1]
  else:data=git(BASE+a['adapter'],H)
  assert sha(data)==a['sha256'];effective[name]=sha(data)
 assert read(d/'source-effective.json')==effective
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
  pattern='^('+'|'.join(n for n in required if '/' not in n)+')$'
  assert run['argv'][1:]==['-test.v','-test.run='+pattern]
  assert build['argv'][build['argv'].index('-o')+1]==run['argv'][0]
  assert not names['SKIP'];assert not r['census']['duplicates'];assert not r['census']['unexpected'];assert not r['census']['vacuous']
  missing=[n for n in required if names['PASS'].count(n)!=1];assert missing==r['census']['missing']
  rows.append({'package':package,'build_exit':r['build_exit'],'run_exit':r['exit'],'sha256':sha(binary),'expected_top':sum('/' not in n for n in required),'passed_top':sum('/' not in n for n in names['PASS']),'failed':names['FAIL'],'skipped':names['SKIP'],'required_children':sum('/' in n for n in required),'passed_required_children':sum('/' in n and names['PASS'].count(n)==1 for n in required),'all_pass_children':sum('/' in n for n in names['PASS']),'missing_passes':missing,'build_command':build,'run_command':run,'verdict':'PASS' if r['exit']==0 and not missing else 'FAIL'})
 out['modes'][mode]={'expected_top':sum(x['expected_top'] for x in rows),'passed_top':sum(x['passed_top'] for x in rows),'suites':rows,'tool_files':len(tools),'source_effective_files':len(effective),'verdict':'FAIL'}
write('audit.json',out)
# Preserve exact inspected source/harness bytes as inert .txt files, not Go packages.
paths=['src/core/runtime/engine_int_tinygo_test.go','src/core/runtime/engine.go','src/core/runtime/invocation_other.go','src/core/runtime/trampoline_tinygo_arm64.go','src/wago/instance_call.go','src/wago/api.go','src/wago/config.go','src/wago/gc_stage8_tinygo_guest_amd64_test.go','src/core/compiler/optimization/catalog.go']
(P/'_inspected').mkdir(exist_ok=True)
for path in paths:(P/'_inspected'/(path.replace('/','__')+'.txt')).write_bytes(git(path))
for name in ['harness.py','suite-plan.json','toolchain.json','cancellation_test.go.in','foreign_stack_arm64_test.go.in']:(P/'_inspected'/name).write_bytes(git(BASE+name,H))
write('inspected-source-manifest.json',{p.name:sha(p.read_bytes()) for p in (P/'_inspected').iterdir()})
print(json.dumps({m:{'expected':v['expected_top'],'passed':v['passed_top'],'verdict':v['verdict']} for m,v in out['modes'].items()},indent=2))
