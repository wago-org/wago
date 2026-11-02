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


def census(events, target):
    expected = set(SPEC['tests'][target])
    passed = [e['Test'] for e in events if e.get('Test') and '/' not in e['Test'] and e.get('Action') == 'pass']
    failures = [e for e in events if e.get('Action') in ('fail', 'skip')]
    children = [e['Test'] for e in events if e.get('Test') and e.get('Action') == 'pass']
    return len(passed) == len(expected) and set(passed) == expected and not failures and (target != 'darwin' or all(children.count(name) == 1 for name in SPEC['graph_subtests']))


def main():
    p = argparse.ArgumentParser(description=__doc__)
    p.add_argument('source', type=Path)
    p.add_argument('output', type=Path)
    p.add_argument('--mode', choices=['ordinary', 'wagodebug', 'wago_regalloccheck'], required=True)
    p.add_argument('--target', choices=['darwin', 'windows'], required=True)
    a = p.parse_args()
    arch = {'darwin': 'arm64', 'windows': 'amd64'}[a.target]
    if (platform.system().lower(), platform.machine().lower()) != (a.target, {'arm64': 'arm64', 'amd64': 'amd64'}[arch]):
        p.error('requires actual native ' + a.target + '/' + arch + '; Linux/crossbuild does not qualify')
    if a.target == 'darwin' and subprocess.run(['sysctl', '-in', 'sysctl.proc_translated'], capture_output=True, text=True).stdout.strip() == '1':
        p.error('Rosetta is not native execution')
    ident = validate(os.environ.get('QUALIFICATION_SOURCE', ''), os.environ.get('QUALIFICATION_DRIVER', ''))
    root, out = a.source.resolve(), a.output.resolve()
    harness = Path(capture(['git', 'rev-parse', '--show-toplevel'], HERE)).resolve()
    if root == harness or root in harness.parents or harness in root.parents or root == out or root in out.parents or harness == out or harness in out.parents:
        p.error('source, harness and receipts must be independent')
    if capture(['git', 'rev-parse', 'HEAD'], root) != SPEC['source'] or ident['source'] != SPEC['source']:
        p.error('wrong source pin')
    if capture(['git', 'status', '--porcelain', '--untracked-files=all'], root):
        p.error('dirty source')
    for name, sha in SPEC['source_files'].items():
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
    write(out / 'host.json', {**ident, 'target': a.target, 'machine': platform.machine(), 'python': sys.version,
        'go': capture(['go', 'version'], root), 'go_sha256': digest(Path(shutil.which('go'))),
        'driver_sha256': digest(Path(__file__)), 'contracts_sha256': digest(REV/'contracts.json')})
    fixture = 'graph_test.go' if a.target == 'darwin' else 'windows_admission_test.go'
    write(out/'overlay.json', {'Replace': {str(root/'src/wago/qualification_supplement_test.go'): str(REV/'_fixtures'/fixture)}})
    flags = ['-overlay='+str(out/'overlay.json')] + ([] if a.mode == 'ordinary' else ['-tags='+a.mode])
    env = dict(os.environ, GOTOOLCHAIN='local', CGO_ENABLED='0', GOWORK='off')
    if a.target == 'darwin':
        (out/'generated').mkdir()
        env['RFW_EXPORT'] = str(out/'generated')
    commands = [(['go', 'list', *flags, '-json', '-test', './src/wago'], 'package-census.jsonl'),
                (['go', 'test', *flags, '-json', '-count=1', '-timeout=15m', '-run=^('+'|'.join(SPEC['tests'][a.target])+')$', './src/wago'], 'tests.jsonl')]
    codes = []
    write(out/'commands.json', {'commands': commands, 'environment': {k: env[k] for k in ['GOTOOLCHAIN','CGO_ENABLED','GOWORK']}, 'expected': SPEC['tests'][a.target]})
    for command, log in commands:
        with (out/log).open('w') as f:
            codes.append(subprocess.run(command, cwd=root, env=env, stdout=f, stderr=subprocess.STDOUT).returncode)
    events = []
    for line in (out/'tests.jsonl').read_text().splitlines():
        try: events.append(json.loads(line))
        except ValueError: pass
    graph_ok = a.target != 'darwin' or all((out/'generated'/n).exists() and digest(out/'generated'/n) == SPEC['fixtures']['_fixtures/'+n] for n in ['graph.wasm','scalar.wasm','noGC.wasm'])
    unchanged = before == manifest(root) and driver == manifest(harness) and not capture(['git', 'status', '--porcelain', '--untracked-files=all'], root)
    ok = codes == [0, 0] and census(events, a.target) and graph_ok and unchanged
    write(out/'source-manifest.json', before)
    write(out/'driver-manifest.json', driver)
    write(out/'summary.json', {'status': 'PASS' if ok else 'FAIL', 'source': SPEC['source'], 'driver': ident['driver'],
        'native_execution': True, 'exit_codes': codes, 'exact_census': census(events, a.target), 'graph_hashes': graph_ok,
        'unchanged': unchanged, 'terminal_events': [e for e in events if e.get('Action') in ('pass','skip','fail')],
        'ceiling': 'Windows storage/admission only; no Windows converted GC JIT claim. Darwin candidate graphs only; baseline and TinyGo remain NOT_RUN.'})
    return 0 if ok else 1


if __name__ == '__main__':
    sys.exit(main())
