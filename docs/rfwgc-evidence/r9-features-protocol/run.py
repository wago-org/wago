#!/usr/bin/env python3
"""Execute independent probes in disposable immutable git archives (no worktrees).

Feature failures are deliberately reported as failures, not expected-pass tests.
The fixture suffix prevents known out-of-scope defects from changing root gates.
"""
import argparse
import io
import json
from pathlib import Path
import shutil
import subprocess
import tarfile
import tempfile

PINS = {
    'main': '90f2b57dafa7f8c3cc7e3aca38b91148f15e1d1e',
    'port': 'ca30843801956bae67d5cd1219f66d191518c125',
    'pr1020': 'ce63d517a55f2981b7ccf6e150d1b9050fb5f953',
    'tune': '504dcae5a626403407158e5aade6442de1563787',
}
p = argparse.ArgumentParser()
p.add_argument('--go', required=True, help='absolute Go executable; avoids archive mise trust')
p.add_argument('--only', choices=PINS)
p.add_argument('--output', type=Path, required=True)
a = p.parse_args()
fixture_dir = Path(__file__).resolve().parent
repo = subprocess.check_output(['git', 'rev-parse', '--show-toplevel'], text=True).strip()
a.output.mkdir(parents=True, exist_ok=True)
for label, pin in PINS.items():
    if a.only and label != a.only:
        continue
    with tempfile.TemporaryDirectory(prefix='wago-r9-features-protocol-') as tmp:
        root = Path(tmp)
        archive = subprocess.check_output(['git', '-C', repo, 'archive', pin])
        with tarfile.open(fileobj=io.BytesIO(archive)) as tar:
            tar.extractall(root, filter='data')
        feature = label in ('main', 'port')
        pkg = 'src/wago' if feature else 'src/core/runtime/gc/native'
        stem = 'r9_features_regression_test.go' if feature else 'r9_protocol_parallel_test.go'
        shutil.copyfile(fixture_dir / (stem+'.txt'), root/pkg/stem)
        for mode in ('ordinary', 'wagodebug', 'wago_regalloccheck'):
            cmd = [a.go, 'test', './'+pkg, '-run', '^TestR9Features' if feature else '^TestR9Protocol',
                   '-count=1', '-timeout=60s', '-v']
            if mode != 'ordinary':
                cmd += ['-tags='+mode]
            result = subprocess.run(cmd, cwd=root, stdout=subprocess.PIPE, stderr=subprocess.STDOUT, text=True)
            (a.output/f'{label}-{mode}.log').write_text(result.stdout)
            metadata = {'pin': pin, 'command': cmd, 'exit': result.returncode, 'skipped': '--- SKIP:' in result.stdout}
            (a.output/f'{label}-{mode}.json').write_text(json.dumps(metadata, indent=2)+'\n')
            print(label, mode, json.dumps(metadata), flush=True)
            if metadata['skipped']:
                raise SystemExit('unexpected skipped probe')
