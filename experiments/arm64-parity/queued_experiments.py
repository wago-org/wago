"""Run bounded experiment batches under the shared device lock.

Each job releases its reservation. Pressure candidates must pass exact oracles
in both bounds modes before they can enter the timing queue.
"""
import json
import os
from pathlib import Path
import subprocess
import sys
import time

ROOT = Path(__file__).resolve().parents[2]
OUT = Path(__file__).resolve().parent
CORPUS = ROOT.parent.parent / 'Web/wasm.fyi/corpora/applications'
LOCK = Path.home() / 'benchmark-lock.py'
state = {'jobs': [], 'started_at': time.time()}


def run(name, command, env=None):
    job = {'name': name, 'command': command, 'status': 'queued'}
    state['jobs'].append(job)
    def save():
        (OUT / 'queued-experiments-status.json').write_text(json.dumps(state, indent=2)+'\n')
    save()
    args = [sys.executable, str(LOCK), '--wait', '--owner', 'wago-root', '--']
    if env:
        args += ['env'] + [f'{key}={value}' for key,value in env.items()]
    args += command
    with (OUT / f'{name}.txt').open('w') as log:
        job['status'] = 'waiting-or-running'
        save()
        result = subprocess.run(args, cwd=ROOT, stdout=log, stderr=subprocess.STDOUT)
    job['status'] = 'passed' if result.returncode == 0 else 'failed'
    job['exit_code'] = result.returncode
    save()
    print(f'{name}: {job["status"]}', flush=True)
    return result.returncode == 0


def main():
    signals = '/tmp/arm64-parity-pressure-signals.test'
    explicit = '/tmp/arm64-parity-pressure-explicit.test'
    if not run('queue-build-signals', ['go','test','-tags','wago_guardpage','-c','-o',signals,'./experiments/arm64-parity']):
        return 1
    if not run('queue-build-explicit', ['go','test','-c','-o',explicit,'./experiments/arm64-parity']):
        return 1
    base_env = {'WAGO_PARITY_CORPUS': str(CORPUS)}
    subset = '(hardware-prime-implicants|serialization-protobuf|video-dct|geo-point-in-polygon|files-glob-match|stats-linear-regression|ml-inference|vision-dilation)'
    run('queue-residual-subset', [signals,'-test.run=^$',f'-test.bench=BenchmarkParity/{subset}/','-test.benchtime=120ms','-test.count=3'],base_env)
    pressure_subset = '(graphics-reed-solomon|compiler-register-allocation|crypto-aes128|video-dct)'
    pressure_source = (ROOT/'src/core/compiler/backend/railshot/arm64/interval_region.go').read_text()
    floors = ('2','4','5') if 'WAGO_ARM64_EXPERIMENT_INTERVAL_FLOOR' in pressure_source else ()
    if not floors:
        print('Pressure retuning was rejected; restore rejected-region-pressure.patch to reproduce it.', flush=True)
    for floor in floors:
        env = dict(base_env, WAGO_ARM64_EXPERIMENT_INTERVAL_FLOOR=floor)
        okay = True
        for label,binary in [('explicit',explicit),('signals',signals)]:
            okay = run(f'queue-floor-{floor}-{label}-oracles', [binary,'-test.run=^$','-test.bench=BenchmarkParity/.*/Exec','-test.benchtime=1x'],env) and okay
        if not okay:
            print(f'floor {floor}: rejected at correctness gate',flush=True)
            continue
        # Interleave the production floor around each candidate.
        for round_index in range(3):
            for selected in ('3',floor):
                sample_env=dict(base_env,WAGO_ARM64_EXPERIMENT_INTERVAL_FLOOR=selected)
                run(f'queue-floor-{floor}-round-{round_index}-mode-{selected}',[signals,'-test.run=^$',f'-test.bench=BenchmarkParity/{pressure_subset}/','-test.benchtime=120ms'],sample_env)
    if not run('queue-build-profiler',['go','build','-tags','wago_runtime wago_profile wago_guardpage','-o','/tmp/wago-arm64-prof','./cli/wago']):
        return 1
    manifest=json.loads((CORPUS/'manifest.json').read_text())
    for name in ('video-dct','geo-point-in-polygon'):
        w=next(w for w in manifest if w['id']=='applications/'+name)
        capture=OUT/f'profile-{name}-queued'
        if capture.exists():
            print(f'{capture}: existing capture, skipped',flush=True)
            continue
        run('queue-profile-'+name,['/tmp/wago-arm64-prof','profile','record','--backend=samply','--samply=/opt/homebrew/bin/samply','--module='+str(CORPUS/w['artifact']),'--export='+w['export'],'--args='+','.join(map(str,w['args'])),'--want='+','.join(w['oracle']['expected']),'--mode=prepared','--bounds=signals','--duration=3s','--rate=1000','--include-code','--source-maps','--out='+str(capture)])
    return 0

if __name__ == '__main__':
    sys.exit(main())
