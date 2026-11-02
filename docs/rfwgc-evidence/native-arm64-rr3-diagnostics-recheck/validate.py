#!/usr/bin/env python3
"""Bounded committed-blob review and ARM64 cross-build; no native qualification."""
import hashlib, importlib.util, io, json, os, pathlib, subprocess, sys, tarfile, tempfile
ROOT = pathlib.Path(__file__).resolve().parent
REPO = ROOT.parents[2]
H = '4af18b3a2484b901e11f7347adab8e547176dbc6'
OLD = '6c71c4d6747456578f55928e900e36bb2d1449a8'
S = '5847df30f1af15d10b743d90a24b1429b290d358'
P = 'docs/rfwgc-evidence/native-arm64-rr3/'
def blob(rev, path):
    return subprocess.check_output(['git','show',rev+':'+path],cwd=REPO)
def command(cmd,cwd,env=None):
    p=subprocess.run(cmd,cwd=cwd,env=env,stdout=subprocess.PIPE,stderr=subprocess.STDOUT,timeout=180)
    print(json.dumps({'command':list(map(str,cmd)), 'exit':p.returncode}),flush=True)
    print(p.stdout.decode(errors='replace'),flush=True)
    assert p.returncode == 0
    return p.stdout
assert blob(H,'.github/workflows/ci.yml') == blob(OLD,'.github/workflows/ci.yml')
for name in ['plan.json','timer-capability.go.txt']:
    assert blob(H,P+name) == blob(OLD,P+name)
print('Committed workflow, census/source/probe plan and timer unchanged: PASS',flush=True)
with tempfile.TemporaryDirectory(prefix='scratch-',dir=ROOT) as temp:
    temp=pathlib.Path(temp)
    harness=temp/'harness'; harness.mkdir()
    for name in ['run.py','plan.json','test_harness.py']:
        (harness/name).write_bytes(blob(H,P+name))
    env=dict(os.environ,PYTHONDONTWRITEBYTECODE='1')
    command([sys.executable,'-m','unittest','-v','test_harness'],harness,env)
    spec=importlib.util.spec_from_file_location('reviewed_run',harness/'run.py')
    mod=importlib.util.module_from_spec(spec); sys.dont_write_bytecode=True; spec.loader.exec_module(mod)
    source=temp/'source'; source.mkdir()
    data=subprocess.check_output(['git','archive',S,'go.mod','go.sum','src','tests/support','codegen','internal'],cwd=REPO)
    with tarfile.open(fileobj=io.BytesIO(data)) as archive:
        archive.extractall(source,filter='data')
    go=pathlib.Path(subprocess.check_output(['go','env','GOROOT'],cwd=REPO,text=True).strip())/'bin/go'
    env={k:v for k,v in os.environ.items() if not k.startswith(('GO','CGO','WAGO','QEMU'))}
    env.update(GOOS='linux',GOARCH='arm64',CGO_ENABLED='0',GOTOOLCHAIN='local',GOWORK='off',GOENV='off')
    command([str(go),'version'],source,env)
    package='./src/core/compiler/backend/railshot/arm64'
    for mode in ['ordinary','wagodebug','wago_regalloccheck']:
        for group in ['backend','wago','wago-race','runtime']:
            actual=mod.build_tags(mode,group)
            expected=[] if mode=='ordinary' else [mode]
            if group=='backend': expected.append('wago_codegenstats')
            assert actual == (['-tags='+','.join(expected)] if expected else [])
            print(json.dumps({'mode':mode,'group':group,'tags':actual}),flush=True)
        tags=mod.build_tags(mode,'backend')
        listed=json.loads(command([str(go),'list','-json',*tags,package],source,env))
        assert 'diagnostics_enabled.go' in listed['GoFiles']
        assert 'diagnostics_disabled.go' not in listed['GoFiles']
        binary=temp/(mode+'.test')
        command([str(go),'test','-c',*tags,'-o',str(binary),package],source,env)
        assert mod.elf(binary)==183
        print(json.dumps({'mode':mode,'elf_machine':183,'sha256':hashlib.sha256(binary.read_bytes()).hexdigest(),'native_execution':'NOT_RUN'}),flush=True)
print('PASS: all three backend cross-builds and tag-selection contracts; native execution NOT_RUN',flush=True)
