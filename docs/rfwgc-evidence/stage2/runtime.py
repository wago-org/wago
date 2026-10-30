#!/usr/bin/env python3
"""Portable Stage 1/2 runtime harness. No benchmark is run with --check-only.
Copy only the self-contained harness into an archive when --revision is supplied.
Artifacts live in TemporaryDirectory and are removed, including on failure.
"""
import argparse
import os
from pathlib import Path
import shutil
import subprocess
import tempfile


def run(root, go, env, args):
    command = [go, 'test', './src/wago', '-run', '^$', '-timeout=10m']
    if not args.check_only:
        command += ['-bench', '^BenchmarkGCLifetimeRuntime$', '-benchmem',
                    '-count', str(args.count), '-benchtime', args.benchtime]
    print('cwd:', root, flush=True)
    print('command:', ' '.join(command), flush=True)
    subprocess.run(command, cwd=root, env=env, check=True)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--revision', help='git revision to archive, e.g. 6ceb86221')
    parser.add_argument('--check-only', action='store_true')
    parser.add_argument('--count', type=int, default=10)
    parser.add_argument('--benchtime', default='1s')
    args = parser.parse_args()
    repo = Path(__file__).resolve().parents[3]
    # Query inside the trusted checkout; use its actual Go binary/environment
    # in the untrusted temporary archive rather than invoking a mise shim there.
    goroot = subprocess.check_output(['go', 'env', 'GOROOT'], cwd=repo, text=True).strip()
    go = str(Path(goroot) / 'bin/go')
    env = dict(os.environ, GOROOT=goroot, GOMAXPROCS='8', GOFLAGS='-p=8', GOWORK='off')
    if not args.revision:
        run(repo, go, env, args)
        return
    with tempfile.TemporaryDirectory(prefix='wago-lifetime-runtime-') as directory:
        archive = subprocess.Popen(['git', 'archive', args.revision], cwd=repo,
                                   stdout=subprocess.PIPE)
        try:
            subprocess.run(['tar', '-x', '-C', directory], stdin=archive.stdout, check=True)
        finally:
            archive.stdout.close()
        if archive.wait():
            raise RuntimeError('git archive failed')
        harness = Path('src/wago/gc_lifetime_stage2_amd64_test.go')
        shutil.copyfile(repo / harness, Path(directory) / harness)
        run(directory, go, env, args)


if __name__ == '__main__':
    main()
