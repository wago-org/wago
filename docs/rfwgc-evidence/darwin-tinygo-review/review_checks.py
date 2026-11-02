#!/usr/bin/env python3
import ast, hashlib, importlib.util, io, json, os, subprocess, sys, tarfile
from pathlib import Path
R=Path(__file__).resolve().parent; E=R.parent/'darwin-tinygo-supplement'; repo=R.parents[2]
sys.dont_write_bytecode=True
spec=importlib.util.spec_from_file_location('harness',E/'harness.py'); h=importlib.util.module_from_spec(spec);spec.loader.exec_module(h)
def digest(b):return hashlib.sha256(b).hexdigest()
def git(*args):return subprocess.check_output(['git',*args],cwd=repo)
def save(n,v):(R/n).write_text(json.dumps(v,indent=2,sort_keys=True)+'\n')
commit='a60b1a57f9b973952747177d541480a21b65ea01'
paths=git('ls-tree','-r','--name-only',commit,'--',str(E.relative_to(repo))).decode().splitlines()
assert not any('/_cache/' in n or '/_export/' in n or '/_source/' in n for n in paths)
identities={n:h.sha(repo/n) for n in paths};assert all(digest(git('show',commit+':'+n))==v for n,v in identities.items())
save('identities.json',{'reviewed_commit':commit,'source':h.PIN['source'],'files':identities})
for n in ['harness.py','controls.py','validate.py','unpatched-control.py']:ast.parse((E/n).read_text())
archive=E/'tinygo0.42.0.darwin-arm64.tar.gz';assert h.sha(archive)==h.PIN['archive_sha256']
release=json.loads((E/'release.json').read_text());asset=next(a for a in release['assets'] if a['name']==archive.name)
assert asset['digest']=='sha256:'+h.PIN['archive_sha256']
with tarfile.open(archive) as t:
 for n,v in {**h.PIN['before'],'bin/tinygo':h.PIN['compiler_sha256']}.items():assert digest(t.extractfile('tinygo/'+n).read())==v
h.check_hashes(E/'_cache/tinygo',h.PIN['after'])
source_bytes=git('archive',h.PIN['source']); count=0
with tarfile.open(fileobj=io.BytesIO(source_bytes)) as t:
 for m in t:
  if m.isfile():
   assert digest(t.extractfile(m).read())==h.sha(E/'_source'/m.name),m.name
   count+=1
h.check_hashes(E/'_source',h.PIN['patch'])
for src,dst in [('src/wago/gc_stage8_tinygo_guest_amd64_test.go','src/wago/supplement_stage8_test.go'),('src/wago/design_repair_unsupported_test.go','src/wago/supplement_rejection_test.go')]:
 assert (E/'_source'/src).read_bytes().split(b'\n',1)[1]==(E/'_source'/dst).read_bytes().split(b'\n',1)[1]
for src,dst in [('foreign_stack_arm64_test.go.in','src/core/runtime/supplement_foreign_stack_arm64_test.go'),('cancellation_test.go.in','src/wago/supplement_cancellation_test.go')]:assert (E/src).read_bytes()==(E/'_source'/dst).read_bytes()
plan=json.loads((E/'suite-plan.json').read_text());counts={};controls=[]
for mode in h.MODES:
 assert plan[mode]==h.suites(mode)
 counts[mode]=sum('/' not in n for names in plan[mode].values() for n in names)
 for pkg,names in plan[mode].items():
  good=''.join('--- PASS: '+n+'\n' for n in names);assert h.census(good,names)['ok']
  for kind,bad in [('missing',good.replace('--- PASS: '+names[-1]+'\n','')),('duplicate',good+good),('skip',good+'--- SKIP: TestBad\n'),('failure',good+'--- FAIL: TestBad\n'),('unexpected',good+'--- PASS: TestUnexpected\n'),('earlyreturn',good+'unavailable under TinyGo')]:
   assert not h.census(bad,names)['ok'];controls.append([mode,pkg,kind])
assert list(counts.values())==[33,36,33]
cross=[]
for row in json.loads((E/'_local-validation-2/results.json').read_text()):
 idx=list(h.suites(row['mode'])).index(row['package']);p=E/'_local-validation-2'/f"{row['mode']}-{idx}.test"
 data=p.read_bytes();assert digest(data)==row['sha256'];assert data[:8]==bytes.fromhex('cffaedfe0c000001');assert all(n.encode() in data for n in row['required'] if '/' not in n);assert row['exit']==0
 cross.append({'mode':row['mode'],'package':row['package'],'sha256':digest(data),'execution':'NOT_RUN'})
assert len(cross)==9
save('checks.json',{'archive_publisher_digest_matches_retained_release':True,'compiler_and_pristine_hashes':True,'patched_hashes':True,'exact_source_files_compared':count,'source_archive_sha256':digest(source_bytes),'retag_bodies_unchanged':True,'counts':counts,'census_negative_controls':controls,'cross_artifacts':cross,'tracked_tool_trees':[]})
# Run bounded existing negative controls (they copy patch files into temporary trees).
p=subprocess.run([sys.executable,str(E/'controls.py'),str(R/'_controls')],cwd=repo,capture_output=True,text=True,timeout=180)
(R/'controls.log').write_text(p.stdout+p.stderr);assert p.returncode==0
# Go excludes underscore/hidden private trees from discovery in every mode.
go='/home/dev/.local/share/mise/installs/go/1.27.1/bin/go'
env=dict(os.environ,GOTOOLCHAIN='local',GOENV='off',GOWORK='off',GOFLAGS='-mod=readonly -p=2')
discovery=[]
for mode in h.MODES:
 cmd=[go,'list'];cmd+=[] if mode=='ordinary' else ['-tags='+mode];cmd+=['./docs/rfwgc-evidence/darwin-tinygo-supplement/...']
 p=subprocess.run(cmd,cwd=repo,env=env,capture_output=True,text=True,timeout=60);assert p.returncode==0 and not p.stdout.strip(),p.stderr
 discovery.append({'mode':mode,'exit':p.returncode,'stdout':p.stdout,'stderr':p.stderr})
save('discovery.json',discovery)
actionlint='/home/dev/.local/share/mise/installs/actionlint/1.7.7/actionlint'
p=subprocess.run([actionlint,str(E/'workflow.proposal.yml')],capture_output=True,text=True,timeout=60);(R/'actionlint.log').write_text(p.stdout+p.stderr);assert p.returncode==0
assert all(h.sha(repo/n)==v for n,v in identities.items())
save('result.json',{'local_checks':'PASS','native_darwin_tinygo':'NOT_RUN','activation':'NO','supplement_files_unchanged':True,'no_cross_rebuild':True})
print('PASS local identity, syntax, archive, census, cross-artifact, negative and discovery checks; native NOT_RUN; activation NO')
