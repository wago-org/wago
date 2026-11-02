#!/usr/bin/env python3
"""Exact-source native qualification. Never adapts tests or runtime semantics."""
import argparse
import collections
import hashlib
import json
import os
from pathlib import Path
import platform
import re
import shutil
import signal
import subprocess
import sys

HERE = Path(__file__).resolve().parent
PLAN = json.loads((HERE / 'plan.json').read_text())
SOURCE = PLAN['source']


def build_tags(mode, group):
    tags = [] if mode == 'ordinary' else [mode]
    if group == 'backend':
        tags.append('wago_codegenstats')
    return ['-tags=' + ','.join(tags)] if tags else []


def sha(path):
    return hashlib.sha256(Path(path).read_bytes()).hexdigest()


def identity(source, harness, actual_source, actual_harness):
    if source != SOURCE:
        raise ValueError('wrong source pin')
    if not re.fullmatch('[0-9a-f]{40}', harness):
        raise ValueError('harness must be a full SHA')
    if actual_source != source or actual_harness != harness:
        raise ValueError('checkout HEAD mismatch')


def native(host, env, machine):
    if host != ('Linux', 'aarch64') or machine != 183:
        raise ValueError('native Linux ARM64 required; emulation is not qualification')
    if env.get('RUNNER_OS') != 'Linux' or env.get('RUNNER_ARCH') != 'ARM64':
        raise ValueError('GitHub native ARM64 runner identity required')
    for key, value in env.items():
        if (key in ('GOOS', 'GOARCH', 'GOFLAGS', 'GOEXPERIMENT', 'GODEBUG', 'GOGC',
                    'GOMEMLIMIT', 'GOMAXPROCS', 'CGO_ENABLED', 'LD_PRELOAD', 'LD_LIBRARY_PATH', 'CC', 'CXX')
                or key.startswith(('QEMU', 'WAGO_', 'CGO_', 'GOARM'))):
            if value:
                raise ValueError('inherited execution override: ' + key)
    if any('mise' in p and 'shims' in p for p in env.get('PATH', '').split(':')):
        raise ValueError('mise shims are not permitted')


def elf(path):
    data = Path(path).read_bytes()[:20]
    if data[:6] != b'\x7fELF\x02\x01':
        raise ValueError('expected little endian ELF64: ' + str(path))
    return int.from_bytes(data[18:20], 'little')


def census(text, names, package):
    events = [json.loads(line) for line in text.splitlines() if line.strip()]
    terminals = collections.Counter()
    starts = collections.Counter()
    bad = []
    packages = []
    all_started, all_passed = collections.Counter(), collections.Counter()
    for event in events:
        if event.get('Package') != package:
            bad.append(event)
        test = event.get('Test')
        action = event.get('Action')
        if action in ('fail', 'skip'):
            bad.append(event)
        if test and action == 'run':
            all_started[test] += 1
        if test and action == 'pass':
            all_passed[test] += 1
        if test and '/' not in test:
            if action == 'run':
                starts[test] += 1
            if action == 'pass':
                terminals[test] += 1
        if not test and action in ('pass', 'fail', 'skip'):
            packages.append(action)
    expected = collections.Counter(names)
    return {'ok': starts == expected and terminals == expected and not bad and packages == ['pass']
                  and all_started == all_passed and all(n == 1 for n in all_passed.values()),
            'expected': names, 'started': dict(starts), 'passed': dict(terminals),
            'all_test_passes': dict(all_passed), 'bad_events': bad, 'package_results': packages}


class Run:
    def __init__(self, out, cwd, env):
        self.out, self.cwd, self.env = out, cwd, env
        self.rows = []

    def command(self, label, cmd, timeout=300, cwd=None):
        row = {'label': label, 'command': list(map(str, cmd)), 'cwd': str(cwd or self.cwd),
               'timeout_seconds': timeout, 'exit': None}
        self.rows.append(row)
        self.save()
        with (self.out / (label + '.log')).open('wb') as log:
            try:
                proc = subprocess.Popen(row['command'], cwd=row['cwd'], env=self.env,
                                        stdout=log, stderr=subprocess.STDOUT, start_new_session=True)
                try:
                    row['exit'] = proc.wait(timeout=timeout)
                except subprocess.TimeoutExpired:
                    os.killpg(proc.pid, signal.SIGKILL)
                    proc.wait()
                    row['exit'] = 124
            except OSError as exc:
                log.write(str(exc).encode())
                row['exit'] = 127
        self.save()
        return row['exit'], (self.out / (label + '.log')).read_text(errors='replace')

    def save(self):
        (self.out / 'commands.json').write_text(json.dumps(self.rows, indent=2) + '\n')

    def checked(self, label, cmd, timeout=300, cwd=None):
        code, output = self.command(label, cmd, timeout, cwd)
        if code:
            raise ValueError(label + ' failed: ' + str(code))
        return output.strip()


def manifest(run, root, label):
    # Independently compare every checked-out blob with Git, not only status or mtimes.
    run.checked(label + '-diff', ['git', 'diff', '--exit-code', 'HEAD', '--'], cwd=root)
    status = run.checked(label + '-status', ['git', 'status', '--porcelain', '--untracked-files=all'], cwd=root)
    if status:
        raise ValueError(label + ' dirty checkout: ' + status)
    tree = run.checked(label + '-tree', ['git', 'ls-tree', '-rz', 'HEAD'], cwd=root)
    files = {}
    for entry in tree.split('\0'):
        if not entry:
            continue
        header, path = entry.split('\t', 1)
        mode, kind, oid = header.split()
        if kind == 'commit':  # Deliberately uninitialized, unused conformance gitlinks.
            continue
        file = root / path
        data = os.readlink(file).encode() if mode == '120000' else file.read_bytes()
        actual = hashlib.sha1(b'blob ' + str(len(data)).encode() + b'\0' + data).hexdigest()
        if actual != oid:
            raise ValueError('Git blob mismatch: ' + path)
        files[path] = hashlib.sha256(data).hexdigest()
    (run.out / (label + '-manifest.json')).write_text(json.dumps(files, sort_keys=True, indent=2) + '\n')
    return files


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument('--source', type=Path, required=True)
    parser.add_argument('--out', type=Path, required=True)
    parser.add_argument('--mode', choices=['ordinary', 'wagodebug', 'wago_regalloccheck'], required=True)
    args = parser.parse_args()
    source = args.source.resolve()
    harness = HERE.parents[2]
    out = args.out.resolve()
    out.mkdir(parents=True, exist_ok=False)
    run = Run(out, source, os.environ.copy())
    result = {'qualified': False, 'source': SOURCE, 'harness': os.environ.get('QUALIFICATION_DRIVER'),
              'mode': args.mode, 'native_execution': 'NOT_RUN', 'census': {},
              'job_ceiling_minutes': 45, 'matrix_ceiling_minutes': 135}
    before = {}
    try:
        identity(os.environ.get('QUALIFICATION_SOURCE'), os.environ.get('QUALIFICATION_DRIVER', ''),
                 run.checked('source-head', ['git', 'rev-parse', 'HEAD']),
                 run.checked('harness-head', ['git', 'rev-parse', 'HEAD'], cwd=harness))
        for label, root in [('source', source), ('harness', harness)]:
            before[label] = manifest(run, root, label + '-before')
        for path, digest in PLAN['probe_sha256'].items():
            if sha(source / path) != digest:
                raise ValueError('original probe hash mismatch: ' + path)
        native((platform.system(), platform.machine()), os.environ, elf('/proc/self/exe'))
        host = {'uname': list(os.uname()), 'python_elf_machine': elf('/proc/self/exe'),
                'runner_os': os.environ['RUNNER_OS'], 'runner_arch': os.environ['RUNNER_ARCH'],
                'cpuinfo': Path('/proc/cpuinfo').read_text(),
                'clock': __import__('time').get_clock_info('monotonic').__dict__}
        (out / 'host.json').write_text(json.dumps(host, indent=2) + '\n')
        go = Path(shutil.which('go')).resolve()
        if elf(go) != 183:
            raise ValueError('Go executable is not ARM64')
        result['go_executable'] = {'path': str(go), 'sha256': sha(go), 'elf_machine': elf(go)}
        goroot = go.parent.parent
        run.env.update(GOROOT=str(goroot), GOTOOLCHAIN='local', GOENV='off', GOWORK='off')
        goenv = json.loads(run.checked('go-env', [go, 'env', '-json']))
        if any(goenv[k] != v for k, v in {'GOVERSION': 'go1.27.1', 'GOOS': 'linux',
               'GOARCH': 'arm64', 'GOHOSTOS': 'linux', 'GOHOSTARCH': 'arm64',
               'GOROOT': str(goroot), 'CGO_ENABLED': '1'}.items()):
            raise ValueError('wrong native Go toolchain or race compiler unavailable')
        # Existing CI installer verifies the ARM64 release archive hash; install only
        # inside the separate harness checkout's ignored .tools directory.
        bootstrap = harness / 'scripts/bootstrap-wabt.sh'
        if sha(bootstrap) != PLAN['bootstrap_sha256']:
            raise ValueError('WABT bootstrap differs from pinned source')
        if sha(HERE / 'timer-capability.go.txt') != PLAN['timer_control_sha256']:
            raise ValueError('timer control differs from archived diagnosis')
        run.checked('wabt-verify', ['sh', bootstrap, '--verify'], cwd=harness)
        wabt = Path(run.checked('wabt-path', ['sh', bootstrap, '--print-path'], cwd=harness))
        if elf(wabt) != 183:
            raise ValueError('WABT executable is not ARM64')
        result['wabt_executable'] = {'path': str(wabt), 'sha256': sha(wabt), 'elf_machine': elf(wabt)}
        (out / 'wabt-provenance.txt').write_bytes((wabt.parent.parent / '.wago-provenance').read_bytes())
        run.env['PATH'] = str(go.parent) + ':' + str(wabt.parent) + ':/usr/bin:/bin'
        run.env.update(WAGO_WAST2JSON=str(wabt), WAGO_WABT_VERSION='1.0.41')
        ok = True
        # Unarmed native clock/signal capability control, independent of original tests.
        timer_source = out / 'timer-capability.go'
        timer_source.write_bytes((HERE / 'timer-capability.go.txt').read_bytes())
        timer = out / 'timer-control'
        code, _ = run.command('timer-build', [go, 'build', '-o', timer, timer_source])
        if code == 0:
            if elf(timer) != 183:
                raise ValueError('timer control is not ARM64')
            code, text = run.command('timer-control', [timer], 30)
            rows = re.findall(r'arch=arm64 clock=1 notify=(\d+) signo=(\d+) tid=\d+ size=64 errno=(\d+)', text)
            control_ok = code == 0 and collections.Counter(rows) == collections.Counter(
                (str(n), str(s), '0') for n in [1, 0, 4] for s in [35, 64])
        else:
            control_ok = False
        result['timer_control_ok'] = control_ok
        ok &= control_ok
        # Separate binaries expose ELF identity; no -exec adapter and no source overlay.
        for group, package, race, labels in [
            ('wago', './src/wago', False, ['focused', 'guard']),
            ('wago-race', './src/wago', True, ['focused', 'guard']),
            ('runtime', './src/core/runtime', False, ['runtime']),
            ('backend', './src/core/compiler/backend/railshot/arm64', False, ['backend']),
        ]:
            binary = out / (group + '.test')
            code, _ = run.command(group + '-build', [go, 'test', '-c', *build_tags(args.mode, group),
                                 *(['-race'] if race else []), '-o', binary, package])
            if code:
                ok = False
                continue
            if elf(binary) != 183:
                raise ValueError('test executable is not ARM64')
            result.setdefault('binaries', {})[group] = {'sha256': sha(binary), 'elf_machine': 183}
            for label in labels:
                suite = PLAN['suites'][label]
                regex = '^(' + '|'.join(suite['tests']) + ')$'
                name = group + '-' + label
                cwd = source / package
                code, listed = run.command(name + '-list', [binary, '-test.list=' + regex], 30, cwd)
                if code or sorted(listed.splitlines()) != suite['tests']:
                    ok = False
                    result['census'][name] = {'ok': False, 'error': 'compiled test listing mismatch', 'listed': listed}
                    continue
                code, text = run.command(name, [go, 'tool', 'test2json', '-p', package,
                    binary, '-test.v=test2json', '-test.count=1', '-test.timeout=3m', '-test.run=' + regex], 240, cwd)
                try:
                    check = census(text, suite['tests'], package)
                except (ValueError, TypeError) as exc:
                    check = {'ok': False, 'error': str(exc)}
                check['exit'] = code
                result['census'][name] = check
                ok &= code == 0 and check['ok']
                result['native_execution'] = 'RUN'
                (out / 'summary.json').write_text(json.dumps(result, indent=2) + '\n')
        result['qualified'] = ok and len(result['census']) == 6
    except Exception as exc:
        result['error'] = str(exc)
    finally:
        for label, root in [('source', source), ('harness', harness)]:
            if label in before:
                try:
                    after = manifest(run, root, label + '-after')
                    same = before[label] == after
                    result[label + '_unchanged'] = same
                    result['qualified'] &= same
                except Exception as exc:
                    result[label + '_integrity_error'] = str(exc)
                    result['qualified'] = False
        (out / 'summary.json').write_text(json.dumps(result, indent=2) + '\n')
        print(json.dumps(result, indent=2))
    return 0 if result['qualified'] else 1


if __name__ == '__main__':
    sys.exit(main())
