"""Run bounded wide-table pin candidates through correctness before focused timing."""
import os
import pathlib
import subprocess
import time

ROOT = pathlib.Path(__file__).resolve().parents[2]
OUT = ROOT / 'experiments/arm64-parity'
LOCK = pathlib.Path.home() / 'benchmark-lock.py'
CORPUS = ROOT / '../../Web/wasm.fyi/corpora/applications'

def run(name, command, extra=None):
    env = dict(os.environ, WAGO_PARITY_CORPUS=str(CORPUS.resolve()))
    env.update(extra or {})
    with (OUT / (name + '.txt')).open('w') as log:
        result = subprocess.run(['python3', str(LOCK), '--wait', '--owner', 'wago-root', '--', *command], cwd=ROOT, env=env, stdout=log, stderr=subprocess.STDOUT)
    time.sleep(10)
    return result.returncode == 0

binaries = {}
for bounds, tags in [('explicit', None), ('signals', 'wago_guardpage')]:
    binary = '/tmp/arm64-parity-wide-pins-' + bounds + '.test'
    command = ['go', 'test', '-c', '-o', binary]
    if tags: command += ['-tags', tags]
    command += ['./experiments/arm64-parity']
    if not run('wide-pins-build-' + bounds, command): raise SystemExit(1)
    binaries[bounds] = binary

for pins in ['2', '4', '6']:
    passing = True
    for bounds, binary in binaries.items():
        passing = run('wide-pins-' + pins + '-' + bounds + '-oracles', [binary, '-test.run', '^$', '-test.bench', 'BenchmarkParity/.*/Exec$', '-test.benchtime=1x'], {'WAGO_ARM64_EXPERIMENT_WIDE_PINS': pins}) and passing
    if not passing: continue
    for round_number in range(3):
        order = ['0', pins] if round_number % 2 == 0 else [pins, '0']
        for setting in order:
            run('wide-pins-' + pins + '-round-' + str(round_number) + '-setting-' + setting, [binaries['signals'], '-test.run', '^$', '-test.bench', 'BenchmarkParity/(video-dct|graphics-reed-solomon|ml-inference|vision-dilation)/(Compile|Exec)$', '-test.benchtime=120ms'], {'WAGO_ARM64_EXPERIMENT_WIDE_PINS': setting})
