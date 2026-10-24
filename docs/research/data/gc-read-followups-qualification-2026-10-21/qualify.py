from pathlib import Path
import json
import os
import subprocess
import time

root = Path('/home/dev/Projects/dewdrop')
tree = root/'.tmp/wago-gc-read-followups-qualification'
public = root/'.tmp/wago-gc-read-followups-public'
out = root/'.tmp/wago-next-gc-perf/gc-read-followups-full-tests'
out.mkdir(exist_ok=False)
env = dict(os.environ, CGO_ENABLED='0', GOMAXPROCS='2', DOTNET_SYSTEM_GLOBALIZATION_INVARIANT='1',
    WAGO_SPEC_INTERPRETER='/home/dev/Projects/wago/.tools/spec-interpreter-9d36019973201a19f9c9ebb0f10828b2fe2374aa/wasm',
    WAGO_SPEC_INTERPRETER_REVISION='9d36019973201a19f9c9ebb0f10828b2fe2374aa')
env.pop('GOWORK', None)
rows = []

def run(label, command, cwd, selected_env=env):
    start = time.monotonic()
    p = subprocess.run(command, cwd=cwd, env=selected_env, capture_output=True, text=True)
    (out/(label+'.log')).write_text(p.stdout+p.stderr)
    row = dict(label=label, command=command, cwd=str(cwd), seconds=time.monotonic()-start,
               exit=p.returncode, stdout=p.stdout, stderr=p.stderr)
    (out/(label+'.json')).write_text(json.dumps(row, indent=2)+'\n')
    rows.append(row)
    (out/'samples.json').write_text(json.dumps(rows, indent=2)+'\n')
    print(json.dumps({key: row[key] for key in ['label','seconds','exit']}), flush=True)
    if p.returncode:
        print((p.stdout+p.stderr)[-12000:], flush=True)
        raise RuntimeError(label+' failed; full diagnostics retained')

run('spec-corpora', ['git','submodule','update','--init','--','tests/conformance/spec-v1','tests/conformance/spec-v2','tests/conformance/spec-v3'], tree)
for label, tags in [('public-normal', []), ('public-checked-stats', ['-tags=wago_codegenstats,wago_regalloccheck'])]:
    run(label, ['go','test','-p','2',*tags,'-count=1','-run','^Test(GCResolverSelectedSpaceDescriptor|GCResolverDependentSpaceFixture|GCTrappingGuard|GCFinalCastArrayGet|GCFinalResolver|GCNumericLeafInline)','./src/wago'], public)
run('combined-normal', ['go','test','-p','2','-count=1','./...'], tree)
run('combined-checked', ['go','test','-p','2','-tags=wago_regalloccheck','-count=1','./...'], tree)
run('combined-bench-checked', ['go','test','-p','2','-tags=wago_regalloccheck','-count=1','./...'], tree/'bench', dict(env,GOWORK='off'))
print('All full qualification checks passed.', flush=True)
