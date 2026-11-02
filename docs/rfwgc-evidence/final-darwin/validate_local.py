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
import contracts
import discriminators

HERE = Path(__file__).resolve().parent
ROOT = HERE.parents[2]
MODES = ['ordinary', 'wagodebug', 'wago_regalloccheck']
BASE = '041f7ed4c376e2a33ce21b22075cd617ddca6597'


def run(argv, **kwargs):
    result = subprocess.run(argv, text=True, capture_output=True, **kwargs)
    assert result.returncode == 0, (argv, result.stdout, result.stderr)
    return {'argv': argv, 'exit_code': result.returncode, 'stdout': result.stdout, 'stderr': result.stderr}


def main():
    report = {'darwin_execution': 'NOT_RUN', 'darwin_compile_only': 'NOT_RUN'}
    plan = json.loads((HERE / 'plan.json').read_text())
    report['source'] = plan['source']
    report['driver_files_sha256'] = {str(path.relative_to(ROOT)): hashlib.sha256(path.read_bytes()).hexdigest() for path in [HERE/'plan.json', HERE/'identity.py', HERE/'contracts.py', HERE/'run.py', HERE/'setup.sh', HERE/'tinygo_setup.py', HERE/'discriminators.py', HERE/'validate_local.py', HERE/'validate_exports.py', HERE/'validate_broad_tags.py', ROOT/'.github/workflows/ci.yml', ROOT/'tests/integration/cipolicy/workflows_test.go', contracts.REV/'contracts.json']}
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
            assert identity.validate(plan['source'], head, head)['driver'] == head
            for source, driver in [('08f511fac41a77f4c0f9046f88ebc78ad05a5da6', head), ('main', head), (plan['source'][:9], head), ('0'*40, head), (plan['source'], 'main'), (plan['source'], '0'*40)]:
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
    for name in before.keys() - {'darwin-qualification', 'windows-qualification'}:
        actual = after[name]
        assert body(before[name]) == body(actual), name
    assert current == baseline, 'workflow including native ARM/TinyGo contracts must remain bytewise unchanged'
    assert "default: 'f2bb" not in current
    assert "QUALIFICATION_HARNESS: ${{ inputs.harness_sha }}" in after['darwin-qualification']
    concurrency = lambda text: text.split('concurrency:\n', 1)[1].split('\nenv:',1)[0]
    assert concurrency(current) == concurrency(baseline)
    assert 'timeout-minutes: 360' in after['darwin-qualification']
    assert 'timeout-minutes: 30' in after['windows-qualification']
    assert 'timeout-minutes: 45' in after['native-arm64-rr3']
    assert 'timeout-minutes: 3' in after['darwin-qualification-summary']
    special = after['darwin-qualification-summary'].split('    steps:\n', 1)[1]
    script = textwrap.dedent(special.split('        run: |\n', 1)[1])
    with tempfile.TemporaryDirectory() as tmp:
        checked = []
        for darwin in ['success', 'failure', 'cancelled', 'skipped']:
            for windows in ['success', 'failure', 'cancelled', 'skipped']:
                for normal_state in ['skipped', 'success', 'failure', 'cancelled']:
                    states = {name: {'result': 'skipped'} for name in before if name != 'ci-ok'}
                    states['changes']['result'] = normal_state
                    states['darwin-qualification'] = {'result': darwin}
                    states['windows-qualification'] = {'result': windows}
                    env = dict(os.environ, DARWIN_RESULT=darwin, WINDOWS_RESULT=windows, NORMAL_RESULTS=json.dumps(states), GITHUB_STEP_SUMMARY=tmp+'/summary')
                    result = subprocess.run(['bash', '-e', '-o', 'pipefail', '-c', script], env=env, text=True, capture_output=True)
                    expected = darwin == windows == 'success' and normal_state == 'skipped'
                    assert (result.returncode == 0) == expected, states
                    checked.append({'darwin': darwin, 'windows': windows, 'normal': normal_state, 'accepted': expected})
        report['explicit_summary_states'] = checked
    report['special_summary_states'] = 'PASS: only both native matrices successful with all normal jobs skipped accepted'
    report['workflow_contracts'] = 'PASS: every normal and native ARM64 job and concurrency group bytewise unchanged from 041f (no source-pin normalization); Darwin source default requires explicit input; ARM 138, Mac 1080 and Windows 90 job-minute ceilings unchanged'
    report['planner_all_modes'] = []
    for mode in MODES:
        tags = [] if mode == 'ordinary' else ['-tags=' + mode]
        report.setdefault('aggregate_policy_all_modes', []).append(run(['go', 'test', *tags, './tests/integration/cipolicy', '-count=1'], cwd=ROOT))
        report['planner_all_modes'].append(run(['go', 'test', *tags, './tests/tools/ci-plan', '-count=1'], cwd=ROOT))
        states = {name: {'result': 'success'} for name in before if name != 'ci-ok'}
        states['darwin-qualification'] = {'result': 'skipped'}
        states['windows-qualification'] = {'result': 'skipped'}
        states['darwin-qualification-summary'] = {'result': 'skipped'}
        env = dict(os.environ, CI_PROFILE='full', CI_CODE='true', CI_CORPUS_SOURCES='true', CI_NEEDS=json.dumps(states))
        report['planner_all_modes'].append(run(['go', 'run', *tags, './tests/tools/ci-plan', 'verify'], cwd=ROOT, env=env))
    report['workflow_lint'] = run(['go', 'run', 'github.com/rhysd/actionlint/cmd/actionlint@v1.7.7', '.github/workflows/ci.yml'], cwd=ROOT)
    report['shell_syntax'] = run(['bash', '-n', str(HERE / 'setup.sh')])
    for name in ['identity.py', 'run.py', 'contracts.py', 'validate_local.py', 'tinygo_setup.py', 'discriminators.py', 'validate_exports.py', 'validate_broad_tags.py']:
        compile((HERE / name).read_text(), str(HERE / name), 'exec')
    report['python_syntax'] = 'PASS'
    spec = contracts.SPEC
    assert spec['source'] == plan['source']
    for name, sha in spec['source_files'].items():
        data = subprocess.check_output(['git', 'show', plan['source']+':'+name], cwd=ROOT)
        assert hashlib.sha256(data).hexdigest() == sha, name
    for name, sha in spec['fixtures'].items():
        assert contracts.digest(contracts.REV/name) == sha, name
    original_graph = subprocess.check_output(['git', 'show', plan['source']+':src/wago/rfwgc_stage3_measurement_bench_test.go'], cwd=ROOT, text=True)
    excerpt = original_graph.split('func BenchmarkRFWStage3Guest', 1)[0].replace('//go:build linux && amd64 && !tinygo && !wago_guardpage', '//go:build darwin && arm64 && !tinygo && !wago_guardpage', 1)
    # This optimization exists only on AMD64; preserve every other fixture byte.
    assert excerpt.count('.WithOptimization("gc-lifetime-analyze", true)') == 1
    excerpt = excerpt.replace('.WithOptimization("gc-lifetime-analyze", true)', '')
    assert (contracts.REV/'_fixtures/graph_test.go').read_text() == excerpt
    for name, fixture in spec['darwin_test_overlays'].items():
        original = subprocess.check_output(['git','show', plan['source']+':'+name], cwd=ROOT, text=True)
        actual = (contracts.REV/fixture).read_text()
        if name.endswith(('design_repair_unsupported_test.go','design_repair_unsupported_token_test.go')):
            for old, replacements in spec['migration'].items():
                if 'func '+old+'(' in original:
                    assert 'func '+old+'(' in actual
                    for test in replacements:
                        assert 't.Run("'+test+'", '+test+')' in actual
        elif name.endswith('gc_lifecycle_regression_test.go'):
            assert 'func awaitGCLifecycle'+original.split('func awaitGCLifecycle',1)[1].split('\nfunc Test',1)[0] in actual
        else:
            adapted = original.replace('//go:build linux && (amd64 || arm64)', '//go:build darwin && arm64', 1)
            assert actual == run(['gofmt'], input=adapted)['stdout']
    for requested in ['', 'main', '0'*40]:
        try: identity.validate(plan['source'], '1'*40, requested)
        except ValueError: pass
        else: raise AssertionError('bad explicit harness pin accepted')
    baseline_events = [{'Test':'TestRFWStage3MeasurementGuest','Action':'pass'}]+[{'Test':n,'Action':'pass'} for n in spec['baseline_graph_subtests']]
    assert contracts.census(baseline_events, 'darwin', True)
    assert not contracts.census(baseline_events[:-1], 'darwin', True)
    assert not contracts.census(baseline_events+[baseline_events[-1]], 'darwin', True)
    assert not contracts.census(baseline_events+[{'Test':'TestRFWStage3MeasurementGuest/graph/true','Action':'pass'}], 'darwin', True)
    for target in ['darwin','windows']:
        events = [{'Test': name, 'Action': 'pass'} for name in spec['tests'][target]]
        if target == 'darwin':
            events += [{'Test': name, 'Action': 'pass'} for name in spec['graph_subtests']]
            events += [{'Test': old+'/'+test, 'Action': 'pass'} for old, tests in spec['migration'].items() for test in tests]
        assert contracts.census(events, target)
        if target == 'darwin':
            assert not contracts.census(events[:-1], target)
        assert not contracts.census(events[1:], target)
        assert not contracts.census(events+[{'Action':'skip','Test':'unexpected'}], target)
        assert not contracts.census(events+[events[0]], target)
        for mode in MODES:
            with tempfile.TemporaryDirectory() as tmp:
                out = Path(tmp)/'receipts'
                proc = subprocess.run([sys.executable,str(HERE/'contracts.py'),str(ROOT),str(out),'--mode',mode,'--target',target],capture_output=True,text=True)
                assert proc.returncode == 2 and 'requires actual native' in proc.stderr and not out.exists()
    for mode in MODES:
        narrow = discriminators.lanes(mode)
        assert len(narrow) == 5
        assert [len(x['expected_tests']) for x in narrow] == [1, 1, 10, 2, 1]
        assert '-race' in narrow[3]['argv']
        assert narrow[4]['environment'] == {'WAGO_SHARED_SCALAR': '0'}
        assert any(x.startswith('-tags=wago_codegenstats') for x in narrow[4]['argv'])
        for lane in narrow:
            assert '-count=1' in lane['argv'] and not any(x.startswith('-bench') for x in lane['argv'])
            events = [{'Test': n, 'Action': 'pass'} for n in lane['expected_tests']]
            if lane['discriminator'] == 'core3-child':
                payload = dict(protocol='2', fixture=discriminators.FIXTURE, nonce=discriminators.NONCE,
                               modules_passed=10, modules_failed=0, modules_skipped=0,
                               assertions_passed=18, assertions_failed=0, assertions_skipped=0)
                event = {'Action': 'output', 'Output': discriminators.MARKER+json.dumps(payload)+'\n'}
                assert not discriminators.evaluate(lane, events, {'commands': []})[1]
                assert discriminators.evaluate(lane, events+[event], {'commands': []})[1]
                assert not discriminators.evaluate(lane, events+[event,event], {'commands': []})[1]
                for key, value in [('modules_passed',9), ('assertions_passed',17), ('modules_skipped',1), ('assertions_skipped',1), ('nonce','wrong')]:
                    bad = dict(payload, **{key: value})
                    assert not discriminators.evaluate(lane, events+[dict(event,Output=discriminators.MARKER+json.dumps(bad))], {'commands': []})[1]
                raw = {'Action':'output', 'Output':'spectest_exec_test.go:1833: gc/const-expr-gc.wast.wast:96 module instantiate rejected: unsupported publication\n'}
                commands = {'commands':[{'type':'module_definition','filename':'commands.5.wasm','line':90}, {'type':'module_instance','line':96}]}
                detail, _ = discriminators.evaluate(lane, events+[raw], commands)
                assert detail['rejection_sites'][0]['commands'][0]['definition'] == 'commands.5.wasm'
                events.append(event)
            assert discriminators.evaluate(lane, events, {'commands': []})[1]
            assert not discriminators.evaluate(lane, events[1:], {'commands': []})[1]
            assert not discriminators.evaluate(lane, events+[events[0]], {'commands': []})[1]
            assert not discriminators.evaluate(lane, events+[{'Action':'skip','Test':'unexpected'}], {'commands': []})[1]
    assert "if: ${{ !cancelled() && steps.darwin-setup.outcome == 'success' }}" in after['darwin-qualification']
    assert 'continue-on-error' not in after['darwin-qualification']
    report['discriminators'] = 'PASS: all-mode exact census; missing/duplicate/wrong nonce/gapped child rejected; rejection source-site mapping; safe continuation retains failed step'
    report['native_contracts'] = 'PASS: pinned hashes, exact/negative census, six Linux refusals; no native execution'
    (contracts.REV / 'local-validation.json').write_text(json.dumps(report, indent=2) + '\n')
    print('PASS: local contracts; Darwin remains NOT_RUN')


if __name__ == '__main__':
    main()
