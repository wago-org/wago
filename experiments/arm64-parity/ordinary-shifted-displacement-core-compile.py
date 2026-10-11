"""Four reversed process pairs for the narrowed candidate's eight changed cores.

Run through benchmark-lock.py. Uses a frozen candidate test binary and the
original full contract IDs; does not rebuild or alter production source.
"""
from pathlib import Path
import json
import os
import re
import subprocess

root = Path('experiments/arm64-parity')
cache = Path('/Users/work/Code/Web/wasm.fyi/.wasmbench/corpus-cache')
manifest = json.loads((cache / 'manifest.json').read_text())['workloads']
files = [line.split()[1] for line in (root / 'ordinary-shifted-displacement-native-changes.txt').read_text().splitlines() if line.startswith('core ')]
assert len(files) == 8
rows = []
out = root / 'ordinary-shifted-displacement-core-compile.jsonl'
for file in files:
    workload = next(w for w in manifest if w['id'].replace('/', '__') + '.bin' == file)
    assert workload['abi'] == 'core' and workload['oracle']['kind'] == 'exact_u64', workload['id']
    for pair in range(4):
        for enabled in [bool(pair % 2), not bool(pair % 2)]:
            env = os.environ.copy()
            env.update(WAGO_SHARED_SCALAR='0', WAGO_PARITY_CACHE=str(cache), WAGO_PARITY_CORE_ID=workload['id'], WAGO_ARM64_EXPERIMENT_ORDINARY_SHIFTED_IMMEDIATE=str(int(enabled)))
            result = subprocess.run(['/tmp/parity-ordinary-shifted-displacement.test', '-test.run=^$', '-test.bench=BenchmarkCachedContract/Compile$', '-test.benchtime=100ms', '-test.count=1'], env=env, text=True, capture_output=True)
            rawfile = root / ('ordinary-shifted-displacement-core-compile-' + workload['id'].replace('/', '__') + f'-{pair}-{int(enabled)}.txt')
            rawfile.write_text(result.stdout + result.stderr)
            result.check_returncode()
            match = re.search(r'BenchmarkCachedContract/Compile-\d+\s+\d+\s+([0-9.]+) ns/op\s+(\d+) B/op\s+(\d+) allocs/op', result.stdout)
            assert match, result.stdout
            rows.append(dict(workload=workload['id'], phase='compile', round=pair, on=enabled, us=float(match[1])/1000, bytes_per_op=int(match[2]), allocations_per_op=int(match[3])))
            out.write_text(''.join(json.dumps(row) + '\n' for row in rows))
    print(workload['id'] + ': completed', flush=True)
