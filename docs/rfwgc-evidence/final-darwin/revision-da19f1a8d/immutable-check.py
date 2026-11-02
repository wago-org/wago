#!/usr/bin/env python3
"""Verify the Git export and policy/planner with the proposed workflow only."""
import gzip, hashlib, json, os, pathlib, subprocess, tempfile
HERE = pathlib.Path(__file__).resolve().parent
ROOT = HERE.parents[3]
PIN = 'da19f1a8d29fec7305d937a42622a198b8e79717'
GO = '/home/dev/.local/share/mise/installs/go/1.27.1/bin/go'
base = pathlib.Path(tempfile.mkdtemp(prefix='darwin-harness-archive-', dir='/home/dev'))
source = base/'source'; source.mkdir()
report = {'source': PIN, 'export': str(source), 'go': GO, 'go_sha256': hashlib.sha256(pathlib.Path(GO).read_bytes()).hexdigest(), 'native_darwin': 'NOT_RUN', 'commands': []}
def save(): (HERE/'immutable-check.json').write_text(json.dumps(report,indent=2)+'\n')
save()
archive = subprocess.Popen(['git','archive',PIN],cwd=ROOT,stdout=subprocess.PIPE)
subprocess.run(['tar','-x','-C',str(source)],stdin=archive.stdout,check=True)
archive.stdout.close(); assert archive.wait()==0
entries = subprocess.check_output(['git','ls-tree','-rz',PIN],cwd=ROOT).split(b'\0')
manifest = {}
for entry in entries:
 if not entry: continue
 meta, name = entry.split(b'\t',1); mode, kind, oid = meta.split(); name = name.decode()
 if kind != b'blob': continue
 path = source/name
 data = os.readlink(path).encode() if path.is_symlink() else path.read_bytes()
 assert hashlib.sha1(b'blob '+str(len(data)).encode()+b'\0'+data).hexdigest() == oid.decode(), name
 manifest[name] = hashlib.sha256(data).hexdigest()
with gzip.open(HERE/'source-manifest.json.gz','wt') as f: json.dump(manifest,f,sort_keys=True)
report['verified_blobs'] = len(manifest)
# Policy must inspect the proposed workflow, not its archived predecessor.
workflow = source/'.github/workflows/ci.yml'
workflow.write_bytes((ROOT/'.github/workflows/ci.yml').read_bytes())
report['declared_overlay'] = {'.github/workflows/ci.yml': hashlib.sha256(workflow.read_bytes()).hexdigest()}
env = {k:v for k,v in os.environ.items() if not k.startswith(('WAGO_','GO'))}
env.update(PATH=str(pathlib.Path(GO).parent)+':/usr/bin:/bin',GOROOT=str(pathlib.Path(GO).parents[1]),GOTOOLCHAIN='local',GOWORK='off',GOCACHE=str(base/'cache'),GOTMPDIR=str(base/'tmp'),GOMAXPROCS='4',CGO_ENABLED='0')
(base/'tmp').mkdir()
report['go_version'] = subprocess.check_output([GO,'version'],env=env,text=True).strip()
for mode in ['ordinary','wagodebug','wago_regalloccheck']:
 for package in ['tests/integration/cipolicy','tests/tools/ci-plan']:
  command = [GO,'test',* ([] if mode=='ordinary' else ['-tags='+mode]),'./'+package,'-count=1']
  log = mode+'-'+package.split('/')[-1]+'.log'
  with (HERE/log).open('w') as f: code = subprocess.run(command,cwd=source,env=env,stdout=f,stderr=subprocess.STDOUT).returncode
  report['commands'].append({'argv':command,'exit':code,'log':log});save()
changed = []
for name, sha in manifest.items():
 path = source/name; data = os.readlink(path).encode() if path.is_symlink() else path.read_bytes()
 expected = report['declared_overlay'].get(name,sha)
 if hashlib.sha256(data).hexdigest()!=expected: changed.append(name)
report['unexpected_changes'] = changed
report['status'] = 'PASS' if not changed and all(c['exit']==0 for c in report['commands']) else 'FAIL'
save()
assert report['status']=='PASS'
