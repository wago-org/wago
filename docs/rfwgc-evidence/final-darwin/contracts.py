#!/usr/bin/env python3
"""Native non-Linux contracts; immutable sources plus declared test-only overlay."""
import argparse
import hashlib
import json
import os
from pathlib import Path
import platform
import shutil
import subprocess
import sys
from identity import validate
from run import capture, manifest, write

HERE = Path(__file__).resolve().parent
REV = HERE / json.loads((HERE / 'plan.json').read_text())['supplement_revision']
SPEC = json.loads((REV / 'contracts.json').read_text())


def digest(path):
    return hashlib.sha256(path.read_bytes()).hexdigest()


def census(events, target, baseline=False):
    expected = {'TestRFWStage3MeasurementGuest'} if baseline else set(SPEC['tests'][target])
    passed = [e['Test'] for e in events if e.get('Test') and '/' not in e['Test'] and e.get('Action') == 'pass']
    failures = [e for e in events if e.get('Action') in ('fail', 'skip')]
    children = [e['Test'] for e in events if e.get('Test') and e.get('Action') == 'pass']
    graph_leaves = [name for name in children if name.startswith('TestRFWStage3MeasurementGuest/') and name.count('/') == 2]
    graph_expected = SPEC['baseline_graph_subtests'] if baseline else SPEC['graph_subtests']
    graph_ok = target != 'darwin' or sorted(graph_leaves) == sorted(graph_expected)
    migration_ok = target != 'darwin' or baseline or all(
        children.count(old+'/'+test) == 1 for old, tests in SPEC['migration'].items() for test in tests)
    return len(passed) == len(expected) and set(passed) == expected and not failures and graph_ok and migration_ok


def darwin_overlays(root):
    replacements = {}
    for name, fixture in SPEC['darwin_test_overlays'].items():
        if digest(root/name) != SPEC['source_files'][name] or digest(REV/fixture) != SPEC['fixtures'][fixture]:
            raise ValueError('test overlay identity mismatch: '+name)
        replacements[str(root/name)] = str(REV/fixture)
    return replacements


def main():
    p = argparse.ArgumentParser(description=__doc__)
    p.add_argument('source', type=Path)
    p.add_argument('output', type=Path)
    p.add_argument('--mode', choices=['ordinary', 'wagodebug', 'wago_regalloccheck'], required=True)
    p.add_argument('--target', choices=['darwin', 'windows'], required=True)
    p.add_argument('--baseline', action='store_true', help='Exact baseline graph-only supplement')
    a = p.parse_args()
    if a.baseline and a.target != 'darwin':
        p.error('baseline is Darwin only')
    source_pin = SPEC['baseline'] if a.baseline else SPEC['source']
    tests = ['TestRFWStage3MeasurementGuest'] if a.baseline else SPEC['tests'][a.target]
    arch = {'darwin': 'arm64', 'windows': 'amd64'}[a.target]
    if (platform.system().lower(), platform.machine().lower()) != (a.target, {'arm64': 'arm64', 'amd64': 'amd64'}[arch]):
        p.error('requires actual native ' + a.target + '/' + arch + '; Linux/crossbuild does not qualify')
    if a.target == 'darwin' and subprocess.run(['sysctl', '-in', 'sysctl.proc_translated'], capture_output=True, text=True).stdout.strip() == '1':
        p.error('Rosetta is not native execution')
    ident = validate(os.environ.get('QUALIFICATION_SOURCE', ''), os.environ.get('QUALIFICATION_DRIVER', ''), os.environ.get('QUALIFICATION_HARNESS', ''))
    root, out = a.source.resolve(), a.output.resolve()
    harness = Path(capture(['git', 'rev-parse', '--show-toplevel'], HERE)).resolve()
    if root == harness or root in harness.parents or harness in root.parents or root == out or root in out.parents or harness == out or harness in out.parents:
        p.error('source, harness and receipts must be independent')
    if capture(['git', 'rev-parse', 'HEAD'], root) != source_pin or ident['source'] != SPEC['source']:
        p.error('wrong source pin')
    if capture(['git', 'status', '--porcelain', '--untracked-files=all'], root):
        p.error('dirty source')
    for name, sha in ({} if a.baseline else SPEC['source_files']).items():
        if digest(root / name) != sha:
            p.error('source hash mismatch: ' + name)
    for name, sha in SPEC['fixtures'].items():
        if digest(REV / name) != sha:
            p.error('fixture hash mismatch: ' + name)
    if any(os.environ.get(k) for k in ['GOFLAGS', 'GOOS', 'GOARCH', 'GOWORK', 'GOEXPERIMENT', 'WAGO_BOUNDS', 'WAGO_SHARED_SCALAR']):
        p.error('inherited target/build overrides must be empty')
    if capture(['go', 'version'], root) != 'go version go1.27.1 '+a.target+'/'+arch:
        p.error('requires exact native Go 1.27.1')
    if json.loads(capture(['go', 'env', '-json', 'GOOS', 'GOARCH'], root)) != {'GOOS': a.target, 'GOARCH': arch}:
        p.error('Go target mismatch')
    out.mkdir(parents=True, exist_ok=False)
    before, driver = manifest(root), manifest(harness)
    write(out / 'host.json', {**ident, 'source': source_pin, 'qualification_source': ident['source'], 'baseline': a.baseline, 'target': a.target, 'machine': platform.machine(), 'python': sys.version,
        'go': capture(['go', 'version'], root), 'go_env': json.loads(capture(['go','env','-json','GOROOT','GOHOSTOS','GOHOSTARCH'], root)), 'go_sha256': digest(Path(shutil.which('go'))),
        'driver_sha256': digest(Path(__file__)), 'contracts_sha256': digest(REV/'contracts.json')})
    fixture = 'graph_test.go' if a.target == 'darwin' else 'windows_admission_test.go'
    replacements = {str(root/'src/wago/qualification_supplement_test.go'): str(REV/'_fixtures'/fixture)}
    if a.target == 'darwin' and not a.baseline:
        replacements.update(darwin_overlays(root))
    write(out/'overlay.json', {'Replace': replacements})
    flags = ['-mod=readonly', '-overlay='+str(out/'overlay.json')] + ([] if a.mode == 'ordinary' else ['-tags='+a.mode])
    env = dict(os.environ, GOTOOLCHAIN='local', CGO_ENABLED='0', GOWORK='off')
    if a.target == 'darwin':
        (out/'generated').mkdir()
        env['RFW_EXPORT'] = str(out/'generated')
    commands = [(['go', 'list', *flags, '-json', '-test', './src/wago'], 'package-census.jsonl'),
                (['go', 'test', *flags, '-json', '-count=1', '-timeout=15m', '-run=^('+'|'.join(tests)+')$', './src/wago'], 'tests.jsonl')]
    codes = []
    write(out/'commands.json', {'commands': commands, 'environment': {k: env.get(k, '') for k in ['GOTOOLCHAIN','CGO_ENABLED','GOWORK','GOROOT','GOENV']}, 'expected': tests})
    for command, log in commands:
        with (out/log).open('w') as f:
            codes.append(subprocess.run(command, cwd=root, env=env, stdout=f, stderr=subprocess.STDOUT).returncode)
    events = []
    for line in (out/'tests.jsonl').read_text().splitlines():
        try: events.append(json.loads(line))
        except ValueError: pass
    graph_ok = a.target != 'darwin' or all((out/'generated'/n).exists() and digest(out/'generated'/n) == SPEC['fixtures']['_fixtures/'+n] for n in ['graph.wasm','scalar.wasm','noGC.wasm'])
    unchanged = before == manifest(root) and driver == manifest(harness) and not capture(['git', 'status', '--porcelain', '--untracked-files=all'], root)
    ok = codes == [0, 0] and census(events, a.target, a.baseline) and graph_ok and unchanged
    write(out/'source-manifest.json', before)
    write(out/'driver-manifest.json', driver)
    write(out/'summary.json', {'status': 'PASS' if ok else 'FAIL', 'source': source_pin, 'driver': ident['driver'],
        'native_execution': True, 'exit_codes': codes, 'exact_census': census(events, a.target, a.baseline), 'graph_hashes': graph_ok,
        'unchanged': unchanged, 'terminal_events': [e for e in events if e.get('Action') in ('pass','skip','fail')],
        'ceiling': 'Windows storage/admission only; no Windows converted GC JIT claim. Darwin results qualify only the recorded source and leaves. TinyGo remains OPEN.'})
    return 0 if ok else 1


if __name__ == '__main__':
    sys.exit(main())
