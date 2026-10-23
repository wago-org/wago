#!/usr/bin/env python3
"""Checkpointed, fresh-process optimization screening; rerun to resume."""
import argparse
import fcntl
import hashlib
import json
import os
from pathlib import Path
import platform
import subprocess
import time


def digest(path):
    return hashlib.sha256(path.read_bytes()).hexdigest()


def main():
    p = argparse.ArgumentParser(description=__doc__)
    p.add_argument('--binary', type=Path, required=True)
    p.add_argument('--root', type=Path, required=True)
    p.add_argument('--out', type=Path, required=True)
    p.add_argument('--samples', type=int, default=3)
    p.add_argument('--repeat', type=int, default=7)
    p.add_argument('--baseline-only', action='store_true')
    p.add_argument('--options', help='comma-separated optimization subset (default: all)')
    a = p.parse_args()
    if a.samples < 1 or a.repeat < 1:
        p.error('samples and repeat must be positive')
    a.binary = a.binary.resolve()
    a.root = a.root.resolve()
    a.out.mkdir(parents=True, exist_ok=True)
    # Serialize this campaign across invocations. Never infer liveness from a
    # stale PID or state file: the OS releases this lock when its owner exits.
    with (a.out / 'run.lock').open('w') as lock:
        fcntl.flock(lock, fcntl.LOCK_EX | fcntl.LOCK_NB)
        manifest = a.root / 'manifest.json'
        contracts = json.loads(manifest.read_text())
        # Fixed before any measurement: lexicographically first ID per category.
        selected = {}
        for c in sorted(contracts, key=lambda c: c['id']):
            selected.setdefault(c['provenance']['category'], c)
        for c in selected.values():
            if digest(a.root / c['artifact']) != c['sha256']:
                raise ValueError('artifact hash mismatch: ' + c['id'])
        env = dict(os.environ, GOMAXPROCS='1')
        catalog = json.loads(subprocess.check_output([str(a.binary), '-list'], env=env))
        selected_options = catalog
        if a.options is not None:
            names = set(a.options.split(','))
            unknown = names - {o['name'] for o in catalog}
            if unknown:
                p.error('unknown optimizations: ' + ', '.join(sorted(unknown)))
            selected_options = [o for o in catalog if o['name'] in names]
        meta = {
            'binary_sha256': digest(a.binary), 'manifest_sha256': digest(manifest),
            'source_sha': subprocess.check_output(['git', 'rev-parse', 'HEAD'], text=True).strip(),
            'host': platform.node(), 'platform': platform.platform(),
            'repeat': a.repeat, 'catalog': catalog, 'contracts': list(selected.values()),
            'selection': 'lexicographically first ID per category, before measurement',
            'bounds': 'runtime default', 'workers': 1, 'gomaxprocs': 1,
            'quality': 'screening; background load is not controlled',
        }
        if a.options is not None:
            meta['selected_options'] = [o['name'] for o in selected_options]
        meta_path = a.out / 'manifest.json'
        if meta_path.exists():
            if json.loads(meta_path.read_text()) != meta:
                raise ValueError('campaign identity changed; use a new output directory')
        else:
            meta_path.write_text(json.dumps(meta, indent=2) + '\n')
        # One atomic file per sample survives termination, including an interrupted
        # write. Rename follows fsync; abandoned .tmp files are never considered done.
        jobs = [('', True)] if a.baseline_only else [('', True)] + [
            (o['name'], enabled) for o in selected_options for enabled in (False, True)]
        count = 0
        for sample in range(a.samples):
            for ci, c in enumerate(selected.values()):
                ordered = jobs if (sample + ci) % 2 == 0 else list(reversed(jobs))
                for option, enabled in ordered:
                    key = json.dumps([sample, c['id'], option, enabled])
                    dest = a.out / (hashlib.sha256(key.encode()).hexdigest() + '.json')
                    if dest.exists():
                        continue
                    cmd = [str(a.binary), '-root', str(a.root), '-id', c['id'],
                           '-repeat', str(a.repeat)]
                    if option:
                        cmd += ['-option', option, '-enabled=' + str(enabled).lower()]
                    start = time.time()
                    try:
                        result = subprocess.run(cmd, env=env, text=True, capture_output=True, timeout=30)
                        row = {'sample': sample, 'id': c['id'], 'option': option,
                               'enabled': enabled, 'exit_code': result.returncode,
                               'stderr': result.stderr, 'wall_s': time.time() - start}
                        if result.returncode == 0:
                            row['measurement'] = json.loads(result.stdout)
                        else:
                            row['stdout'] = result.stdout
                    except subprocess.TimeoutExpired:
                        row = {'sample': sample, 'id': c['id'], 'option': option,
                               'enabled': enabled, 'error': 'timeout', 'wall_s': time.time() - start}
                    temp = dest.with_suffix('.tmp')
                    with temp.open('w') as f:
                        json.dump(row, f)
                        f.write('\n')
                        f.flush()
                        os.fsync(f.fileno())
                    temp.replace(dest)
                    count += 1
                    if count % 100 == 0 or a.baseline_only:
                        print(f'completed {count} new samples; last={c["id"]}/{option or "default"}', flush=True)
        print(f'finished: {count} new samples', flush=True)


if __name__ == '__main__':
    main()
