#!/usr/bin/env python3
"""Exercise the real RV64 CI script with a recording Go tool, without compiling."""
import collections
import json
import os
import shutil
from pathlib import Path
import subprocess
import tempfile

repo = Path(__file__).resolve().parents[2]
with tempfile.TemporaryDirectory(prefix='wago-rv64-ci-test.') as tmp:
    root = Path(tmp)
    bin_dir = root / 'bin'
    bin_dir.mkdir()
    (bin_dir / 'uname').write_text('#!/bin/sh\nprintf "riscv64\\n"\n')
    (bin_dir / 'go').write_text('''#!/usr/bin/env python3
import json,os,sys
from pathlib import Path
args=sys.argv[1:]
if args == ['env','GOOS']: print('linux'); sys.exit(0)
if args == ['env','GOARCH']: print('riscv64'); sys.exit(0)
if not args or args[0] != 'test': sys.exit(0)
record={'args':args,'artifacts':{}}
for key,value in sorted(os.environ.items()):
 if (key.startswith('WAGO_RV64_') and key.endswith('_ARTIFACT_MODE')) or key == 'WAGO_RV64_PORTABLE_FEATURE_MODE':
  base=key.removesuffix('_MODE')
  directory=os.environ.get(base+'_DIR')
  if directory is None: continue
  path=Path(directory)
  # Only the random workspace prefix differs between all-in-one and split runs.
  parts=path.parts
  index=next(i for i,p in enumerate(parts) if p in ['ordinary','checked'])
  record['artifacts'][key]=value
  record['artifacts'][base+'_DIR']='/'.join(parts[index:])
  if value == 'prepare':
   path.mkdir(parents=True,exist_ok=True); (path/'test-artifact').write_text('prepared')
  elif value == 'load' and not (path/'test-artifact').is_file():
   sys.exit('missing prepared artifact: '+str(path))
with open(os.environ['CAPTURE_GO'],'a') as f: f.write(json.dumps(record,sort_keys=True)+'\\n')
sys.exit(int(os.environ.get('TEST_GO_STATUS','0')))
''')
    for p in bin_dir.iterdir(): p.chmod(0o755)
    serial = 0

    def run(mode, check=None, phase=None, artifact_dir=None, failure=0, expected=0, script=None):
        global serial
        serial += 1
        capture = root / f'capture-{serial}'
        env = {k: v for k, v in os.environ.items() if not k.startswith('WAGO_RV64_')}
        env.update(PATH=str(bin_dir)+os.pathsep+env['PATH'], CAPTURE_GO=str(capture),
                   RUNNER_TEMP=str(root), TEST_GO_STATUS=str(failure))
        for key, value in [('CHECK', check), ('PHASE', phase), ('ARTIFACT_DIR', artifact_dir)]:
            if value is not None: env['WAGO_RV64_CI_'+key] = str(value)
        result = subprocess.run(['bash', str(script or repo/'scripts/ci-riscv64.sh'), mode], cwd=repo,
                                env=env, text=True, stdout=subprocess.PIPE, stderr=subprocess.STDOUT)
        assert result.returncode == expected, (mode, check, phase, result.returncode, result.stdout)
        records = [json.loads(line) for line in capture.read_text().splitlines()] if capture.exists() else []
        return records, result.stdout

    def counts(records):
        return collections.Counter(json.dumps(r, sort_keys=True) for r in records)

    for mode in ['public', 'artifacts']:
        legacy, _ = run(mode, script=os.environ.get('BASELINE_RV64_CI_SCRIPT'))
        assert len(legacy) == (14 if mode == 'public' else 12), (mode, len(legacy))
        split = []
        for check in ['ordinary', 'checked']:
            artifacts = root / (mode+'-'+check)
            prepared, _ = run(mode, check, 'prepare', artifacts)
            assert (artifacts/check/'portable-features/test-artifact').is_file(), 'prepare must retain artifacts'
            received = root / (mode+'-'+check+'-received')
            shutil.copytree(artifacts, received)
            shutil.rmtree(artifacts)
            loaded, _ = run(mode, check, 'load', received)
            assert all(not any('wago_precompiled' in arg for arg in r['args']) for r in prepared)
            assert all(any('wago_precompiled' in arg for arg in r['args']) for r in loaded)
            split += prepared + loaded
        assert counts(split) == counts(legacy), mode+' split must run every original command exactly once'
    legacy, _ = run('guards', script=os.environ.get('BASELINE_RV64_CI_SCRIPT'))
    ordinary, _ = run('guards', 'ordinary')
    checked, _ = run('guards', 'checked')
    assert counts(ordinary+checked) == counts(legacy)
    assert len(ordinary) == len(checked) == 1
    records, _ = run('public', 'ordinary', 'prepare', root/'failure', failure=43, expected=43)
    assert len(records) == 1, 'test failures must stop immediately'
    _, output = run('public', 'invalid', expected=2)
    assert 'check' in output.lower()
    _, output = run('public', 'ordinary', 'invalid', expected=2)
    assert 'phase' in output.lower()
    _, output = run('public', 'ordinary', 'load', expected=2)
    assert 'artifact' in output.lower()
    _, output = run('public', 'ordinary', 'load', root/'absent', expected=1)
    assert 'artifact' in output.lower()
    # Execute the workflow's final status step for success, failure and cancellation.
    workflow = (repo/'.github/workflows/riscv64.yml').read_text()
    gate = workflow.split('  native:\n', 1)[1]
    assert 'needs: [qualification, artifacts]' in gate
    body = gate.split('        run: |\n', 1)[1]
    body = '\n'.join(line[10:] for line in body.splitlines())
    for qualification in ['success', 'failure', 'cancelled', 'skipped']:
        for artifact in ['success', 'failure', 'cancelled', 'skipped']:
            env = dict(os.environ, QUALIFICATION_RESULT=qualification, ARTIFACT_RESULT=artifact)
            result = subprocess.run(['bash', '-euo', 'pipefail', '-c', body], env=env,
                                    text=True, stdout=subprocess.PIPE, stderr=subprocess.STDOUT)
            assert (result.returncode == 0) == (qualification == artifact == 'success'), result.stdout
            if result.returncode: assert 'qualification failed' in result.stdout
print('ci-riscv64 split coverage, artifact handoff, and failure propagation passed')
