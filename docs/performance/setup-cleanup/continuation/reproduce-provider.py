#!/usr/bin/env python3
"""Build matched binaries from isolated worktrees, with an explicit build manifest."""
import argparse
import hashlib
import json
import os
from pathlib import Path
import subprocess
import sys
sys.path.insert(0, str(Path(__file__).resolve().parents[1]))
from measurement import ROOT, capture, digest, fresh_directory


def main():
    p = argparse.ArgumentParser(description=__doc__)
    p.add_argument('work', type=Path, help='new or empty directory, outside the module cache')
    p.add_argument('--provider-base', default='6a6684d2ecd2be2d17e5792733d1d0e03b2f2c0e')
    p.add_argument('--provider-repository', default='https://github.com/wago-org/wasi')
    p.add_argument('--comparison', choices=['provider', 'snapshot'], default='provider')
    p.add_argument('--gomaxprocs', type=int, default=16)
    a = p.parse_args()
    work = a.work.resolve()
    cache = Path(capture(['go', 'env', 'GOMODCACHE'])).resolve()
    if work == cache or cache in work.parents:
        p.error('work directory must be outside the module cache')
    work = fresh_directory(work)
    def run(*cmd, **kwargs):
        subprocess.run(cmd, check=True, **kwargs)
    source = work/'wago'
    run('git', '-C', str(ROOT), 'worktree', 'add', '--detach', str(source), 'HEAD')
    delta = subprocess.check_output(['git', 'diff', '--binary', 'HEAD'], cwd=ROOT)
    (work/'wago-source.patch').write_bytes(delta)
    if delta:
        run('git', '-C', str(source), 'apply', '-', input=delta)
    run('git', 'clone', '--bare', a.provider_repository, str(work/'wasi.git'))
    base = capture(['git', '--git-dir', str(work/'wasi.git'), 'rev-parse', a.provider_base], cwd=work)
    patches = Path(__file__).resolve().parent/'patches'
    patch_names = ['wasi-construction-tests.patch', 'wasi-construction-go122-tests.patch']
    manifest = dict(comparison=a.comparison, wago_commit=capture(['git', 'rev-parse', 'HEAD']), wago_delta_sha256=hashlib.sha256(delta).hexdigest(), provider_base=base,
                    toolchain=capture(['go', 'version']), build_environment=json.loads(capture(['go', 'env', '-json'])),
                    build_tags='wago_guardpage', source_hashes={}, patches={}, commands=[], binaries={})
    for name in patch_names+['wasi-construction.patch']:
        manifest['patches'][name] = digest(patches/name)
    for label in ['baseline', 'candidate']:
        provider = work/f'wasi-{label}'
        run('git', '--git-dir', str(work/'wasi.git'), 'worktree', 'add', '--detach', str(provider), base)
        # A current provider head already contains the lifecycle tests.
        if not (provider/'internal/core/construction_lifecycle_linux_test.go').exists():
            for name in patch_names:
                run('git', '-C', str(provider), 'apply', str(patches/name))
        if a.comparison == 'provider' and label == 'candidate':
            run('git', '-C', str(provider), 'apply', str(patches/'wasi-construction.patch'))
        workspace = work/f'{label}.work'
        workspace.write_text('go 1.22.0\nuse (\n'+''.join(' '+json.dumps(str(v))+'\n' for v in [source, source/'bench', source/'cli/wago-installer', provider])+')\nreplace github.com/wago-org/wago v0.1.0-beta.9 => '+json.dumps(str(source))+'\n')
        cmd = ['go', 'test', '-c', '-tags', 'wago_guardpage', '-o', str(work/f'wasi-{label}.test')]
        if a.comparison == 'snapshot' and label == 'baseline':
            original = work/'original-imports.go'
            original.write_bytes(subprocess.check_output(['git', 'show', '95be283fa511db7b01d86ef72b6c58fbe1ab607a:src/wago/imports.go'], cwd=ROOT))
            overlay = work/'snapshot-baseline.json'
            overlay.write_text(json.dumps({'Replace': {str(source/'src/wago/imports.go'): str(original)}}))
            cmd += ['-overlay', str(overlay)]
        cmd += ['./bench/suite']
        env = dict(os.environ, GOWORK=str(workspace), GOMAXPROCS=str(a.gomaxprocs))
        manifest['commands'].append(cmd)
        manifest.setdefault('build_invocations', []).append(dict(command=cmd, cwd=str(source),
            environment=json.loads(subprocess.check_output(['go', 'env', '-json'], cwd=source, env=env, text=True)),
            runtime_environment={k:env.get(k) for k in ['GOMAXPROCS', 'GOGC', 'GOMEMLIMIT', 'GODEBUG', 'WAGO_BOUNDS']}))
        run(*cmd, cwd=source, env=env)
        manifest['binaries'][label] = dict(path=str(work/f'wasi-{label}.test'), sha256=digest(work/f'wasi-{label}.test'))
        manifest['source_hashes'][label] = {'provider_core': digest(provider/'internal/core/core.go'), 'provider_go_mod': digest(provider/'go.mod'), 'wago_imports': digest(work/'original-imports.go') if a.comparison == 'snapshot' and label == 'baseline' else digest(source/'src/wago/imports.go')}
        manifest.setdefault('dependencies', {})[label] = subprocess.check_output(['go', 'list', '-m', '-json', 'all'], cwd=source, env=env, text=True)
    manifest['benchmark_hashes'] = {str(v.relative_to(source)): digest(v) for v in sorted((source/'bench/suite').glob('*.go'))}
    (work/'build.json').write_text(json.dumps(manifest, indent=2)+'\n')
    print(f'Build manifest: {work / "build.json"}')


if __name__ == '__main__':
    main()
