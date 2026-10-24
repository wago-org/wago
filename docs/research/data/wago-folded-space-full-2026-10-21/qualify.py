import hashlib
import json
import os
from pathlib import Path
import subprocess
import time

root = Path('/home/dev/Projects/dewdrop')
tree = root/'.tmp/wago-folded-space-qualification'
out = root/'.tmp/wago-next-gc-perf/folded-space-qualification'
out.mkdir(exist_ok=False)
env = dict(os.environ, CGO_ENABLED='0', GOMAXPROCS='2', DOTNET_SYSTEM_GLOBALIZATION_INVARIANT='1',
    WAGO_SPEC_INTERPRETER='/home/dev/Projects/wago/.tools/spec-interpreter-9d36019973201a19f9c9ebb0f10828b2fe2374aa/wasm',
    WAGO_SPEC_INTERPRETER_REVISION='9d36019973201a19f9c9ebb0f10828b2fe2374aa')
env.pop('GOWORK', None)
pattern = '^Test(GCResolversLoadSpaceByte|GCResolverSelectedSpaceDescriptor|GCResolverDependentSpaceFixture|GCFinalCastArrayGet|CombinedReferenceABI|GCFinalResolver)'
for label, tags in [('normal', []), ('checked-stats', ['-tags=wago_codegenstats,wago_regalloccheck'])]:
    command = ['go', 'test', '-p', '2', *tags, '-run', pattern, '-count=1',
               './src/core/compiler/backend/railshot/amd64', './src/wago']
    start = time.monotonic()
    p = subprocess.run(command, cwd=tree, env=env, capture_output=True, text=True)
    row = dict(command=command, seconds=time.monotonic()-start, exit=p.returncode,
               stdout=p.stdout, stderr=p.stderr)
    (out/(label+'.json')).write_text(json.dumps(row, indent=2)+'\n')
    print(json.dumps(row), flush=True)
    assert p.returncode == 0
commit = subprocess.check_output(['git', 'rev-parse', 'HEAD'], cwd=tree, text=True).strip()
buildenv = dict(os.environ, WAGO_QUALIFICATION_SHA=commit)
subprocess.run(['python3', str(root/'.tmp/wago-next-gc-perf/prepare-folded-space-host.py')], env=buildenv, check=True)
print('Combined focus and host build complete. No full timings started.', flush=True)
