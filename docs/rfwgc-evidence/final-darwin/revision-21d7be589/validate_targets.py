"""Immutable archive target builds/censuses only; never native qualification."""
import gzip, hashlib, json, os, pathlib, re, subprocess, sys, tarfile, tempfile
HERE = pathlib.Path(__file__).resolve().parent
sys.path.insert(0, str(HERE.parent))
import contracts, discriminators
ROOT = HERE.parents[3]
GO = '/home/dev/.local/share/mise/installs/go/1.27.1/bin/go'
ENV = dict(os.environ, GOROOT=str(pathlib.Path(GO).parent.parent), GOTOOLCHAIN='local', GOWORK='off', GOENV='off', CGO_ENABLED='0')
rows = []
def write(name, data):
    (HERE/name).write_text(json.dumps(data, indent=2)+'\n')
def manifest(folder, pin, name):
    entries = subprocess.check_output(['git','ls-tree','-r',pin], cwd=ROOT, text=True)
    proc = subprocess.Popen(['git','cat-file','--batch'], cwd=ROOT, stdin=subprocess.PIPE, stdout=subprocess.PIPE)
    hashes = {}
    for line in entries.splitlines():
        meta, path = line.split('\t',1)
        mode, kind, oid = meta.split()
        if kind != 'blob': continue
        proc.stdin.write((oid+'\n').encode()); proc.stdin.flush()
        size = int(proc.stdout.readline().split()[2]); data = proc.stdout.read(size)
        assert proc.stdout.read(1) == b'\n'
        assert (folder/path).read_bytes() == data, path
        hashes[path] = hashlib.sha256(data).hexdigest()
    proc.stdin.close(); assert proc.wait() == 0
    (HERE/(name+'.json.gz')).write_bytes(gzip.compress(json.dumps(hashes,sort_keys=True).encode(),mtime=0))
    return hashes
with tempfile.TemporaryDirectory(prefix='qualification-21d7-') as tmp:
    tmp = pathlib.Path(tmp)
    for label, pin in [('source', contracts.SPEC['source']), ('baseline', contracts.SPEC['baseline'])]:
        folder = tmp/label; folder.mkdir()
        archive = subprocess.Popen(['git','archive',pin],cwd=ROOT,stdout=subprocess.PIPE)
        with tarfile.open(fileobj=archive.stdout,mode='r|') as tar: tar.extractall(folder,filter='data')
        assert archive.wait() == 0
        before = manifest(folder,pin,label+'-manifest')
        targets = ['darwin','windows'] if label == 'source' else ['darwin']
        for target in targets:
            baseline = label == 'baseline'
            replacements = contracts.darwin_overlays(folder) if target == 'darwin' and not baseline else {}
            fixture = 'graph_test.go' if target == 'darwin' else 'windows_admission_test.go'
            replacements[str(folder/'src/wago/qualification_supplement_test.go')] = str(HERE/'_fixtures'/fixture)
            overlay = tmp/(label+'-'+target+'.json'); overlay.write_text(json.dumps({'Replace':replacements}))
            for mode in ['ordinary','wagodebug','wago_regalloccheck']:
                flags = [] if mode == 'ordinary' else ['-tags='+mode]
                checks = [('focus', './src/wago', flags, ['TestRFWStage3MeasurementGuest'] if baseline else contracts.SPEC['tests'][target])]
                if target == 'darwin' and not baseline:
                    for lane in discriminators.lanes(mode)[3:]:
                        # Race target requires native Darwin CGO; build ordinary reservation inclusion here only.
                        tags = [x for x in lane['argv'] if x.startswith('-tags=')]
                        checks.append((lane['discriminator'],lane['argv'][-1],tags,lane['expected_tests']))
                for name, package, tags, expected in checks:
                    prefix = '-'.join([label,target,mode,name])
                    env = dict(ENV, GOOS=target, GOARCH='arm64' if target=='darwin' else 'amd64')
                    common = ['-mod=readonly','-overlay='+str(overlay),*tags]
                    argv = [GO,'list',*common,'-json',package]
                    result = subprocess.run(argv,cwd=folder,env=env,text=True,capture_output=True)
                    (HERE/(prefix+'-census.log')).write_text(result.stdout+result.stderr)
                    assert result.returncode == 0, prefix
                    info = json.loads(result.stdout); names = []
                    for file in info['TestGoFiles']:
                        path = str(pathlib.Path(info['Dir'])/file)
                        names += re.findall(r'^func (Test\w+)\(',pathlib.Path(replacements.get(path,path)).read_text(),re.M)
                    assert all(names.count(n)==1 for n in expected), (prefix,expected)
                    binary = tmp/(prefix+'.test')
                    argv = [GO,'test','-c',*common,'-o',str(binary),package]
                    with (HERE/(prefix+'-build.log')).open('w') as log:
                        result = subprocess.run(argv,cwd=folder,env=env,stdout=log,stderr=subprocess.STDOUT)
                    rows.append(dict(label=prefix,argv=argv,exit_code=result.returncode,expected_tests=expected,native_execution=False))
                    write('target-checks.json',rows)
                    assert result.returncode == 0, prefix
                    binary.unlink()
        assert before == manifest(folder,pin,label+'-after')
write('target-result.json',dict(status='PASS',cross_builds=len(rows),native_execution='NOT_RUN',native_race='NOT_RUN: Darwin CGO required',toolchain=subprocess.check_output([GO,'version'],env=ENV,text=True).strip()))
