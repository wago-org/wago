"""Separate final-head delta; never promotes base receipts to final-head execution."""
import pathlib,json,subprocess,tarfile,hashlib,re,gzip
import run as q
E=q.E;R=E.parents[2];BASE=q.M['pin'];PIN='ca164dd25b77c0b7d858fb9c9db125e858a38458'
assert json.loads((E/'process-drain.json').read_text())['drained'], 'Finish and drain base qualification first'
assert not any(r['verdict']=='RUNNING' for r in json.loads((E/'final-summary.json').read_text())['required'])
D=E/'delta-ca164';D.mkdir();W=pathlib.Path(q.M['scratch'])/'delta-ca164';W.mkdir();S=W/'source';S.mkdir()
def sha(p):return hashlib.sha256(p.read_bytes()).hexdigest()
def git(*a):return subprocess.check_output(['/usr/bin/git',*a],cwd=R)
a=W/'source.tar'
with a.open('wb') as f:subprocess.run(['/usr/bin/git','archive',PIN],cwd=R,stdout=f,check=True)
with tarfile.open(a) as t:t.extractall(S,filter='data')
files={}
for row in git('ls-tree','-rz',PIN).split(b'\0'):
 if not row:continue
 meta,name=row.split(b'\t',1);mode,kind,oid=meta.decode().split();name=name.decode()
 if kind=='blob':
  data=git('cat-file','blob',oid);assert (S/name).read_bytes()==data,name
  files[name]=hashlib.sha256(data).hexdigest()
# Exact gitlinks, no live-submodule reuse.
for version in ['v1','v2','v3']:
 rel='tests/conformance/spec-'+version;rev=git('ls-tree',PIN,rel).decode().split()[2]
 assert rev==q.M[version]['pin']
 archive=pathlib.Path(q.M['scratch'])/('spec-'+version+'.tar')
 assert sha(archive)==q.M[version]['archive_sha256']
 with tarfile.open(archive) as t:t.extractall(S/rel,filter='data')
(S/'.tools').mkdir(exist_ok=True)
for name in ['wabt-1.0.41-linux-x64','spec-interpreter-9d36019973201a19f9c9ebb0f10828b2fe2374aa']:(S/'.tools'/name).symlink_to(q.S/'.tools'/name)
subprocess.run(['/usr/bin/git','init','-q',str(S)],check=True)
(S/'.git/objects/info/alternates').write_text(git('rev-parse','--path-format=absolute','--git-common-dir').decode().strip()+'/objects\n')
subprocess.run(['/usr/bin/git','-C',str(S),'read-tree',PIN],check=True);(S/'.git/HEAD').write_text(PIN+'\n')
meta={'base':BASE,'source':PIN,'export':str(S),'archive_sha256':sha(a),'driver_sha256':sha(pathlib.Path(__file__)),'execution_driver_sha256':q.D,'tracked_blobs':len(files),'files_before':files}
(D/'source.json').write_text(json.dumps(meta,indent=2)+'\n')
q.E=D;q.M['pin']=PIN;q.S=S
q.env.update(GOWORK='off',WAGO_SPEC_INTERPRETER=str(S/'.tools/spec-interpreter-9d36019973201a19f9c9ebb0f10828b2fe2374aa/wasm'),WAGO_WAST2JSON=str(S/'.tools/wabt-1.0.41-linux-x64/bin/wast2json'))
# Passing explicit cwd avoids the original module function default binding.
code="import subprocess,sys; p=subprocess.check_output(['git','ls-files','-z']).decode().split('\\0'); p=[x for x in p if x.endswith('.go')]; bad=[]; [bad.extend(subprocess.check_output([sys.argv[1],'-l',*p[i:i+100]],text=True).splitlines()) for i in range(0,len(p),100)]; print('tracked_go_files',len(p)); print('\\n'.join(bad)); sys.exit(bool(bad))"
q.run('format',['/usr/bin/python3','-c',code,q.G+'/bin/gofmt'],S)
required=['TestRRRecheckStorageOnlyReturnBehindDynamicReader','TestRRRecheckCanceledAdmissionBehindDynamicReader','TestRRRecheckSharedMemoryHandoff']
for mode in ['ordinary','wagodebug','wago_regalloccheck']:
 q.test('rr-'+mode,['./src/wago'],mode,['-failfast','-run','^TestRR'],S)
 receipt=json.loads((D/('rr-'+mode)/'result.json').read_text());passed={x.get('Test') for x in receipt['events'] if x['Action']=='pass'}
 assert set(required)<=passed
 (D/('rr-'+mode)/'census.json').write_text(json.dumps({'required':required,'passed':sorted(x for x in passed if x)},indent=2))
 q.test('ci-policy-planner-'+mode,['./tests/integration/cipolicy','./tests/tools/ci-plan'],mode,['-failfast'],S)
q.run('actionlint',[q.G+'/bin/go','run','github.com/rhysd/actionlint/cmd/actionlint@v1.7.7','.github/workflows/ci.yml'],S)
assert all(sha(S/n)==h for n,h in files.items())
assert sha(pathlib.Path(__file__))==meta['driver_sha256']
results=[]
for p in sorted(D.glob('*/result.json')):
 r=json.loads(p.read_text());c=json.loads((p.parent/'command.json').read_text());assert c['source']==r['source']==PIN and r['exit']==0
 assert r['raw_sha256']==hashlib.sha256(gzip.decompress((p.parent/'raw.log.gz').read_bytes())).hexdigest()
 results.append({'suite':p.parent.name,'exit':r['exit'],'raw_sha256':r['raw_sha256']})
assert len(results)==8
(D/'summary.json').write_text(json.dumps({'source':PIN,'base':BASE,'delta_verdict':'PASS','results':results,'source_unchanged':True,'full_suite_executed_on_final_pin':False,'acceptance':'Requires separate full220 base result and parent native-platform decision'},indent=2)+'\n')
