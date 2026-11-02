#!/usr/bin/env python3
"""Linux-safe qualification driver/CI contracts; no Darwin execution claims."""
import hashlib
import json
import os
from pathlib import Path
import re
import subprocess
import sys
import tempfile
import textwrap
from unittest.mock import patch

import identity

HERE = Path(__file__).resolve().parent
ROOT = HERE.parents[2]
MODES = ['ordinary', 'wagodebug', 'wago_regalloccheck']
BASE = 'e8af5e012'


def run(argv, **kwargs):
    result = subprocess.run(argv, text=True, capture_output=True, **kwargs)
    assert result.returncode == 0, (argv, result.stdout, result.stderr)
    return {'argv': argv, 'exit_code': result.returncode, 'stdout': result.stdout, 'stderr': result.stderr}


def main():
    report = {'darwin_execution': 'NOT_RUN', 'darwin_compile_only': 'NOT_RUN'}
    plan = json.loads((HERE / 'plan.json').read_text())
    assert len(plan['lanes']) == 63
    for lane in plan['lanes']:
        receipt = ROOT / lane['receipt']
        assert hashlib.sha256(receipt.read_bytes()).hexdigest() == lane['receipt_sha256']
        assert not any(x.startswith('-bench') for x in lane['argv'])
        original = json.loads(receipt.read_text())['argv']
        expected = ['go'] + [x for x in original[1:] if x != '-failfast']
        assert lane['argv'] == expected, lane['id']
    report['plan_integrity'] = 'PASS: 63 exact receipt commands except Go executable and removed failfast'
    # Real Git identity fixtures prove success plus wrong-source, wrong-driver and dirty refusal.
    with tempfile.TemporaryDirectory() as tmp:
        folder = Path(tmp)
        run(['git', 'init', '-q', tmp])
        (folder / 'fixture').write_text('immutable\n')
        run(['git', '-C', tmp, 'add', 'fixture'])
        run(['git', '-C', tmp, '-c', 'user.name=Harness test', '-c', 'user.email=test@example.invalid', 'commit', '-qm', 'fixture'])
        head = run(['git', '-C', tmp, 'rev-parse', 'HEAD'])['stdout'].strip()
        with patch.object(identity, 'HERE', folder):
            assert identity.validate(plan['source'], head)['driver'] == head
            for source, driver in [('main', head), (plan['source'][:9], head), ('0'*40, head), (plan['source'], 'main'), (plan['source'], '0'*40)]:
                try:
                    identity.validate(source, driver)
                except ValueError:
                    pass
                else:
                    raise AssertionError((source, driver))
            (folder / 'fixture').write_text('dirty\n')
            try:
                identity.validate(plan['source'], head)
            except ValueError:
                pass
            else:
                raise AssertionError('dirty driver accepted')
        refusals = []
        for mode in MODES:
            out = folder / mode
            result = subprocess.run([sys.executable, str(HERE / 'run.py'), str(ROOT), str(out), '--mode', mode], capture_output=True, text=True)
            assert result.returncode == 2 and 'requires actual Darwin arm64' in result.stderr
            assert not out.exists()
            refusals.append({'mode': mode, 'exit_code': result.returncode, 'stderr': result.stderr})
        report['linux_refusal_all_three_modes'] = refusals
    report['identity_contracts'] = 'PASS: exact source, driver HEAD and dirty driver rejection'
    baseline = run(['git', 'show', BASE + ':.github/workflows/ci.yml'], cwd=ROOT)['stdout']
    current = (ROOT / '.github/workflows/ci.yml').read_text()
    def jobs(s):
        return {m.group(1): m.group(0) for m in re.finditer(r'^  ([a-z][\w-]*):\n.*?(?=^  [a-z][\w-]*:\n|\Z)', s.split('jobs:\n', 1)[1], re.M | re.S)}
    before, after = jobs(baseline), jobs(current)
    # Ignore trailing comments separating jobs. Every normal job body remains identical.
    def body(s):
        return re.sub(r'\n(?:  #.*\n|\s*\n)*\Z', '\n', s)
    for name in before.keys() - {'changes', 'ci-ok'}:
        assert body(before[name]) == body(after[name]), name
    normal = "github.event_name != 'workflow_dispatch' || !inputs.darwin_qualification"
    assert after['changes'] == before['changes'].replace("if: github.event_name != 'pull_request' || !github.event.pull_request.draft", f"if: ({normal}) && (github.event_name != 'pull_request' || !github.event.pull_request.draft)")
    assert 'default: false' in current
    assert "if: github.event_name == 'workflow_dispatch' && inputs.darwin_qualification" in after['darwin-qualification']
    assert "group: ci-${{ github.ref }}${{ github.event_name == 'workflow_dispatch' && inputs.darwin_qualification && '-darwin-qualification' || '' }}" in current
    # Remove special summary step/guards/dependency to recover the exact old contract.
    summary = after['ci-ok']
    special = after['darwin-qualification-summary'].split('    steps:\n', 1)[1]
    assert 'name: Darwin qualification summary' in after['darwin-qualification-summary']
    assert "'Full CI not run (Darwin-only dispatch)' || 'CI'" in summary
    assert f'if: always() && ({normal})' in summary
    script = textwrap.dedent(special.split('        run: |\n', 1)[1])
    with tempfile.TemporaryDirectory() as tmp:
        for state in ['success', 'failure', 'cancelled', 'skipped']:
            states = {name: {'result': 'skipped'} for name in before if name != 'ci-ok'}
            states['darwin-qualification'] = {'result': state}
            env = dict(os.environ, DARWIN_RESULT=state, NORMAL_RESULTS=json.dumps(states), GITHUB_STEP_SUMMARY=tmp+'/summary')
            result = subprocess.run(['bash', '-e', '-o', 'pipefail', '-c', script], env=env, text=True, capture_output=True)
            assert (result.returncode == 0) == (state == 'success'), state
        states['darwin-qualification']['result'] = 'success'
        states['changes']['result'] = 'success'
        env.update(DARWIN_RESULT='success', NORMAL_RESULTS=json.dumps(states))
        result = subprocess.run(['bash', '-e', '-o', 'pipefail', '-c', script], env=env, text=True, capture_output=True)
        assert result.returncode != 0, 'unexpected normal job execution accepted'
    report['special_summary_states'] = 'PASS: only successful Darwin with all normal jobs skipped accepted'
    summary = re.sub(r"    name: .*\n    if: always\(\) && .*\n", "    name: CI\n    if: always()\n", summary, count=1)
    summary = summary.replace('        if: ' + normal + '\n', '').replace(', darwin-qualification]', ']')
    assert summary == before['ci-ok']
    report['workflow_contracts'] = 'PASS: unchanged normal job bodies, summary and planner; dispatch-only isolation and concurrency suffix'
    report['planner_all_modes'] = []
    for mode in MODES:
        tags = [] if mode == 'ordinary' else ['-tags=' + mode]
        report['planner_all_modes'].append(run(['go', 'test', *tags, './tests/tools/ci-plan', '-count=1'], cwd=ROOT))
        states = {name: {'result': 'success'} for name in before if name != 'ci-ok'}
        states['darwin-qualification'] = {'result': 'skipped'}
        env = dict(os.environ, CI_PROFILE='full', CI_CODE='true', CI_CORPUS_SOURCES='true', CI_NEEDS=json.dumps(states))
        report['planner_all_modes'].append(run(['go', 'run', *tags, './tests/tools/ci-plan', 'verify'], cwd=ROOT, env=env))
    report['workflow_lint'] = run(['go', 'run', 'github.com/rhysd/actionlint/cmd/actionlint@v1.7.7', '.github/workflows/ci.yml'], cwd=ROOT)
    report['shell_syntax'] = run(['bash', '-n', str(HERE / 'setup.sh')])
    for name in ['identity.py', 'run.py', 'validate_local.py']:
        compile((HERE / name).read_text(), str(HERE / name), 'exec')
    report['python_syntax'] = 'PASS'
    (HERE / 'local-validation.json').write_text(json.dumps(report, indent=2) + '\n')
    print('PASS: local contracts; Darwin remains NOT_RUN')


if __name__ == '__main__':
    main()
