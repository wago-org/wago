#!/usr/bin/env python3
"""Run checked CPU/allocation profiles into a new directory."""
import argparse
import json
from pathlib import Path
import re
from measurement import executable, fresh_directory, identity, rows, run_process, settings


def worker_filter(callers, requested):
    leaf = f'BenchmarkWorkerLifecycleDiagnostic/json-as/callers={callers}/full/public/requested={requested}'
    return leaf, '/'.join('^'+re.escape(part)+'$' for part in leaf.split('/'))


def main():
    p = argparse.ArgumentParser(description=__doc__)
    p.add_argument('kind', choices=['command', 'memory-workers'])
    p.add_argument('--binary', required=True, type=Path)
    p.add_argument('--output', required=True, type=Path)
    p.add_argument('--gomaxprocs', type=int, default=16)
    p.add_argument('--callers', type=int, help='default: GOMAXPROCS; diagnostic supports 1, 4, GOMAXPROCS')
    p.add_argument('--workers', type=int, choices=[0, 1, 2, 4, 8], default=0)
    p.add_argument('--cpus', default='0-15')
    p.add_argument('--cpu-time', default='5s')
    p.add_argument('--allocation-iterations', type=int, default=100)
    p.add_argument('--workers-only', action='store_true')
    a = p.parse_args()
    callers = a.callers if a.callers is not None else a.gomaxprocs
    if callers not in {1, 4, a.gomaxprocs} or a.allocation_iterations < 1:
        p.error('unsupported caller count or invalid iteration count')
    binary = executable(a.binary)
    env, prefix = settings(a.gomaxprocs, a.cpus)
    out = fresh_directory(a.output)
    meta = identity(env, {'profile': binary})
    meta.update(kind=a.kind, callers=callers, requested_workers=a.workers, api='public', cpus=a.cpus, status='running', profiles=[])
    cases = []
    if a.kind == 'command':
        cases = [('imports', 'BenchmarkCommandLifecycleDiagnostic/minimal-wasi/Imports', 'cjson'), ('command', 'BenchmarkCommandExec/cjson', 'cjson')]
    else:
        if not a.workers_only:
            cases.append(('instance', 'BenchmarkInstantiate/utf8proc', 'utf8proc'))
        cases.append(('workers', worker_filter(callers, a.workers)[0], 'json-as'))
    (out/'run.json').write_text(json.dumps(meta, indent=2)+'\n')
    for name, leaf, corpus in cases:
        selector = '/'.join('^'+re.escape(part)+'$' for part in leaf.split('/'))
        for profile in ['cpu', 'alloc']:
            profile_path = out/f'{name}.{profile}'
            flags = ['-test.cpuprofile', str(profile_path), '-test.benchtime', a.cpu_time] if profile == 'cpu' else ['-test.memprofile', str(profile_path), '-test.memprofilerate', '1', '-test.benchtime', f'{a.allocation_iterations}x']
            command = prefix+[str(binary), '-test.run', '^$', '-test.count', '1', '-test.benchmem', '-test.bench', selector, '-wago.corpus', corpus, '-wago.bench.lifecycle', *flags]
            output = out/f'{name}-{profile}.txt'
            record = run_process(command, output, env)
            parsed = rows(output.read_text())
            if set(parsed) != {leaf}:
                raise ValueError(f'expected exactly {leaf}; got {list(parsed)}')
            metrics = parsed[leaf]
            if name == 'workers':
                expected = {'callers': callers, 'gomaxprocs': a.gomaxprocs, 'requested-workers': a.workers}
                if any(metrics.get(k) != v for k, v in expected.items()) or not {'phase-input-workers', 'validation-worker-limit', 'backend-worker-limit'} <= metrics.keys():
                    raise ValueError('worker row does not match requested configuration')
            if not profile_path.is_file() or profile_path.stat().st_size == 0:
                raise ValueError('missing profile')
            record.update(leaf=leaf, filter=selector, metrics=metrics, profile=profile)
            meta['profiles'].append(record)
            (out/'run.json').write_text(json.dumps(meta, indent=2)+'\n')
            print(f'{leaf} ({profile}): {metrics}', flush=True)
    meta['status'] = 'complete'
    (out/'run.json').write_text(json.dumps(meta, indent=2)+'\n')


if __name__ == '__main__':
    main()
