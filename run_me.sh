#!/usr/bin/env bash
# macOS comparison of origin/main and PR #802. No writes to the caller's checkout.
# Requires git, python3 (3.9+), and go; downloads the pinned Go/benchstat tools.
set -euo pipefail
WAGO_RUN_ME_SCRIPT="$(cd "$(dirname "$0")" && pwd)/$(basename "$0")"
export WAGO_RUN_ME_SCRIPT
runner=(python3)
if [[ "$(uname -s)" == Darwin ]] && command -v caffeinate >/dev/null 2>&1; then
    runner=(caffeinate -i python3)
fi
exec "${runner[@]}" - "$@" <<'PY'
import argparse
import csv
import hashlib
import json
import math
import os
from pathlib import Path
import platform
import random
import re
import resource
import shutil
import statistics
import subprocess
import sys
import tarfile
import tempfile
import time
import traceback

PERF_VERSION = 'v0.0.0-20260908200009-22c9c6c9d4da'
CORPUS = 'tiny,fib_rec,many_funcs,xxhash,json-as'
SYNTHETIC = {'small', 'pressure', 'join', 'large', 'deep', 'locals', 'many', 'large_then_small'}
RSS_HELPER = r'''
// Diagnostic sampling only: ps reports current RSS in KiB on macOS.
// getrusage ru_maxrss is bytes on Darwin, unlike Linux's KiB.
// https://github.com/apple/darwin-xnu/blob/main/bsd/man/man2/getrusage.2
func sharingCurrentRSS(t testing.TB) uint64 {
	t.Helper()
	b, err := exec.Command("/bin/ps", "-o", "rss=", "-p", strconv.Itoa(os.Getpid())).Output()
	if err != nil { t.Fatal(err) }
	kib, err := strconv.ParseUint(strings.TrimSpace(string(b)), 10, 64)
	if err != nil { t.Fatal(err) }
	return kib * 1024
}
'''

def sha(path):
    return hashlib.sha256(path.read_bytes()).hexdigest()

def write_json(path, value):
    path.write_text(json.dumps(value, indent=2, sort_keys=True) + '\n')

def write_csv(path, rows):
    keys = list(dict.fromkeys(k for row in rows for k in row))
    with path.open('w', newline='') as f:
        writer = csv.DictWriter(f, fieldnames=keys)
        writer.writeheader()
        writer.writerows(rows)

def replace_once(text, old, new):
    if text.count(old) != 1:
        raise RuntimeError('Memory harness changed; cannot safely adapt: ' + old[:80])
    return text.replace(old, new, 1)

def mac_memory_harness(text):
    text = replace_once(text, '//go:build linux', '//go:build darwin')
    text = replace_once(text, '\t"os"', '\t"os"\n\t"os/exec"')
    text = replace_once(text, '\n\trss, _ := os.ReadFile("/proc/self/statm")',
                        '\n\trssBytes := sharingCurrentRSS(t)')
    start = text.index('\trssFields := strings.Fields(string(rss))')
    end = text.index('\tdata, _ := json.Marshal', start)
    text = text[:start] + text[end:]
    text = replace_once(text, '"statm": string(rss), ', '')
    start = text.index('\t\trss, _ := os.ReadFile("/proc/self/statm")')
    end = text.index('\t\td, _ := json.Marshal', start)
    text = text[:start] + '\t\trssBytes := sharingCurrentRSS(t)\n' + text[end:]
    text = replace_once(text, '"rss_bytes": pages * uint64(os.Getpagesize())',
                        '"rss_bytes": rssBytes')
    text = text.replace('"peak_rss_kib": u.Maxrss', '"peak_rss_kib": u.Maxrss / 1024')
    text = text.replace('syscall.Getrusage(syscall.RUSAGE_SELF, &u)',
                        'if err := syscall.Getrusage(syscall.RUSAGE_SELF, &u); err != nil { t.Fatal(err) }')
    return text + RSS_HELPER

def mac_runtime_harness(text):
    text = replace_once(text, '\t"os"', '\t"os"\n\t"os/exec"')
    start = text.index('\tstatm, err := os.ReadFile("/proc/self/statm")')
    end = text.index('\tcode := 0', start)
    text = text[:start] + '''\trssText, err := exec.Command("/bin/ps", "-o", "rss=", "-p", strconv.Itoa(os.Getpid())).Output()
\tmust(err)
\trssKiB, err := strconv.ParseUint(strings.TrimSpace(string(rssText)), 10, 64)
\tmust(err)
''' + text[end:]
    text = replace_once(text, 'pages * uint64(os.Getpagesize())', 'rssKiB * 1024')
    return replace_once(text, '"peak_rss_kib": u.Maxrss', '"peak_rss_kib": u.Maxrss / 1024')

def extract_json(text):
    for line in text.splitlines():
        pos = line.find('{')
        if pos >= 0:
            try:
                yield json.loads(line[pos:])
            except json.JSONDecodeError:
                pass

def parse_benchmarks(text):
    for line in text.splitlines():
        fields = line.split()
        if len(fields) < 4 or not fields[0].startswith('Benchmark') or not fields[1].isdigit():
            continue
        row = {'benchmark': fields[0], 'iterations': int(fields[1])}
        for i in range(2, len(fields) - 1, 2):
            row[fields[i + 1]] = float(fields[i])
        if 'ns/op' not in row:
            raise RuntimeError('Missing timing: ' + line)
        yield row

def benchmark_name(name):
    # Go omits the CPU suffix at GOMAXPROCS=1. Preserve workload hyphens
    # such as json-as, removing only an actual numeric CPU suffix.
    return re.sub(r'-[0-9]+$', '', name)

def interval(main, pr, rng):
    # Independent fresh processes are the resampling units, never Go iterations.
    ratios = []
    for _ in range(10000):
        a = statistics.median(rng.choices(main, k=len(main)))
        b = statistics.median(rng.choices(pr, k=len(pr)))
        ratios.append(100 * (b / a - 1))
    ratios.sort()
    return ratios[249], ratios[9749]

class Experiment:
    def __init__(self, args):
        self.args = args
        self.out = (Path(args.output).expanduser().resolve() if args.output else
                    Path(tempfile.mkdtemp(prefix='wago-mac-pr802-')))
        if args.output:
            self.out.mkdir(parents=True, exist_ok=False)
        for name in ['logs', 'results', 'harness', 'bin', 'tools', 'profiles']:
            (self.out / name).mkdir()
        self.runs = []
        self.env = dict(os.environ)
        # Ignore caller overrides that would silently change compiler/GC behavior.
        for key in list(self.env):
            if key.startswith(('WAGO_', 'CGO_', 'GOEXPERIMENT')) or key in (
                    'GODEBUG', 'GOOS', 'GOARCH', 'GOWORK', 'GOFLAGS', 'GOMAXPROCS'):
                self.env.pop(key, None)
        self.env.pop('GOROOT', None)
        self.env.update(GOENV='off', GO111MODULE='on', GOWORK='off', GOTOOLCHAIN=args.go_toolchain,
                        GOCACHE=str(self.out / 'go-cache'), GOFLAGS='-buildvcs=false',
                        CGO_ENABLED='0', GOAMD64='v1', GOARM64='v8.0',
                        GOGC='100', GOMEMLIMIT='off', GOMAXPROCS='1',
                        XDG_CACHE_HOME=str(self.out / 'cache'))
        self.repo = self.out / 'source.git'
        self.trees = {rev: self.out / rev for rev in ['main', 'pr']}
        self.auto = args.auto_procs or os.cpu_count() or 1
        self.manifest = {'status': 'preparing', 'settings': vars(args), 'auto_procs': self.auto,
                         'script_sha256': sha(Path(os.environ['WAGO_RUN_ME_SCRIPT']))}
        shutil.copy2(os.environ['WAGO_RUN_ME_SCRIPT'], self.out / 'run_me.sh')
        print('Results directory: ' + str(self.out), flush=True)

    def run(self, name, command, cwd=None, extra=None, kind='setup', revision='', block=0, optional=False):
        command = [str(x) for x in command]
        env = dict(self.env)
        if cwd and (Path(cwd) / 'go.work').exists():
            env['GOWORK'] = str(Path(cwd) / 'go.work')
        elif cwd and (Path(cwd).parent.parent / 'go.work').exists():
            env['GOWORK'] = str(Path(cwd).parent.parent / 'go.work')
        # Explicit per-command overrides must win over workspace discovery.
        # Full-root tests build temporary standalone modules in child processes.
        env.update(extra or {})
        log = self.out / 'logs' / (name + '.txt')
        print(time.strftime('%H:%M:%S') + ' ' + name, flush=True)
        start = time.time()
        with log.open('w') as f:
            proc = subprocess.Popen(command, cwd=cwd, env=env, stdout=f, stderr=subprocess.STDOUT)
            # wait4 gives this child's high-water RSS, not a cumulative maximum
            # of all previously run children. Darwin returns RSS in bytes.
            try:
                _, status, usage = os.wait4(proc.pid, 0)
                proc.returncode = os.waitstatus_to_exitcode(status)
            except BaseException:
                proc.kill()
                proc.wait()
                raise
        row = dict(name=name, kind=kind, revision=revision, block=block, command=command,
                   cwd=str(cwd or Path.cwd()), exit=proc.returncode, started=start,
                   elapsed_seconds=time.time() - start, peak_rss_bytes=usage.ru_maxrss,
                   user_seconds=usage.ru_utime, system_seconds=usage.ru_stime,
                   minor_faults=usage.ru_minflt, major_faults=usage.ru_majflt,
                   GOMAXPROCS=env['GOMAXPROCS'], GOWORK=env.get('GOWORK', ''),
                   TERM=env.get('TERM', ''), workers=env.get('WAGO_SHARING_WORKERS', ''),
                   log=str(log.relative_to(self.out)))
        self.runs.append(row)
        write_json(self.out / 'runs.json', self.runs)
        if proc.returncode and not optional:
            contents = log.read_text()
            failures = [line for line in contents.splitlines()
                        if re.match(r'^(--- FAIL:|FAIL\s|panic:|fatal error:)', line)]
            excerpt = '\n'.join(failures[:20]) + '\n' + contents[-1000:]
            raise RuntimeError(name + ' failed. See ' + str(log) + '\n' + excerpt)
        return log.read_text()

    def setup(self):
        script_root = Path(os.environ['WAGO_RUN_ME_SCRIPT']).parent
        origin = self.args.repo or subprocess.check_output(
            ['git', '-C', str(script_root), 'remote', 'get-url', 'origin'], text=True).strip()
        self.run('git-init', ['git', 'init', '--bare', self.repo])
        self.run('git-remote', ['git', '--git-dir=' + str(self.repo), 'remote', 'add', 'origin', origin])
        self.run('git-fetch', ['git', '--git-dir=' + str(self.repo), 'fetch', '--no-tags', 'origin',
                              '+' + self.args.main_ref + ':refs/heads/main',
                              '+' + self.args.pr_ref + ':refs/heads/pr802'])
        commits = {}
        for rev, ref in [('main', 'main'), ('pr', 'pr802')]:
            commits[rev] = self.run('resolve-' + rev, ['git', '--git-dir=' + str(self.repo),
                                                     'rev-parse', ref + '^{commit}']).strip()
            self.run('checkout-' + rev, ['git', '--git-dir=' + str(self.repo), 'worktree',
                                        'add', '--detach', self.trees[rev], commits[rev]])
        self.manifest.update(origin=origin, commits=commits,
                             host=dict(platform=platform.platform(), machine=platform.machine()))
        # Freeze measurement-only code and fixtures from PR, even if main moves.
        # Copy whole directories so new main-only benchmark files cannot sneak in.
        for relative in ['bench', 'corpus', 'tests/fixtures', 'tests/support/wasmtest']:
            dst = self.trees['main'] / relative
            shutil.rmtree(dst)
            shutil.copytree(self.trees['pr'] / relative, dst)
        harness = self.out / 'harness'
        suite = self.trees['pr'] / 'bench/suite'
        (harness / 'sharing_memory_darwin_test.go').write_text(
            mac_memory_harness((suite / 'sharing_memory_linux_test.go').read_text()))
        (harness / 'sharing_gc_observation_darwin_test.go').write_text(
            replace_once((suite / 'sharing_gc_observation_linux_test.go').read_text(),
                         '//go:build linux', '//go:build darwin'))
        source = self.trees['pr'] / 'docs/experiments/shared-function-regression-data/runtime_memory.go.txt'
        (harness / 'runtime_memory.go').write_text(mac_runtime_harness(source.read_text()))
        for rev, tree in self.trees.items():
            for f in harness.glob('*_test.go'):
                shutil.copy2(f, tree / 'bench/suite' / f.name)
            self.run('gofmt-' + rev, ['gofmt', '-w', *sorted((tree / 'bench/suite').glob('sharing_*darwin_test.go'))])
            self.run('harness-diff-' + rev, ['git', 'diff', '--binary', 'HEAD', '--',
                     'bench', 'corpus', 'tests/fixtures', 'tests/support/wasmtest'], cwd=tree)
        self.run('gofmt-runtime', ['gofmt', '-w', harness / 'runtime_memory.go'])
        for f in self.trees['pr'].glob('bench/suite/sharing_*darwin_test.go'):
            shutil.copy2(f, harness / f.name)
        hashes = {}
        for rev, tree in self.trees.items():
            hashes[rev] = {str(f.relative_to(tree)): sha(f) for directory in (
                'bench', 'corpus', 'tests/fixtures', 'tests/support/wasmtest')
                for f in sorted((tree / directory).rglob('*')) if f.is_file()}
        if hashes['main'] != hashes['pr']:
            raise RuntimeError('Harness/corpus hashes differ between revisions')
        write_json(self.out / 'harness-corpus-sha256.json', hashes)
        write_json(self.out / 'generated-harness-sha256.json', {f.name: sha(f) for f in harness.iterdir()})
        for name, command in [('system', ['sw_vers']), ('cpu', ['sysctl', 'hw.model', 'hw.memsize',
                              'hw.ncpu', 'hw.physicalcpu', 'hw.logicalcpu', 'hw.optional.arm64',
                              'machdep.cpu.brand_string', 'machdep.cpu.features', 'machdep.cpu.leaf7_features',
                              'hw.optional.neon', 'hw.optional.arm.FEAT_AES', 'hw.optional.arm.FEAT_LSE']),
                              ('power-before', ['pmset', '-g', 'custom']),
                              ('battery-before', ['pmset', '-g', 'batt']),
                              ('go-version', ['go', 'version']), ('go-env', ['go', 'env', '-json'])]:
            self.run(name, command, optional=name in ['cpu', 'power-before', 'battery-before'])
        self.run('thermal-before', ['pmset', '-g', 'therm'], optional=True)
        arch = json.loads((self.out / 'logs/go-env.txt').read_text())
        native = 'arm64' if platform.machine() == 'arm64' else 'amd64'
        if arch['GOOS'] != 'darwin' or arch['GOARCH'] != native:
            raise RuntimeError('Go must build for the native Mac architecture')
        self.manifest['architecture'] = native
        self.manifest['production'] = dict(build_tags=[], optimization='default', bounds='explicit default',
            GOGC=100, GOMEMLIMIT='off', CGO_ENABLED=0, GOAMD64='v1', GOARM64='v8.0',
            cpu_affinity='unavailable on macOS; scheduler chooses cores', cache='direct compilation, no code cache')
        self.run('benchstat-install', ['go', 'install', 'golang.org/x/perf/cmd/benchstat@' + PERF_VERSION],
                 extra={'GOBIN': str(self.out / 'tools')})
        self.run('benchstat-version', ['go', 'version', '-m', self.out / 'tools/benchstat'])
        for rev, tree in self.trees.items():
            # Finish every build, download and correctness check before timing.
            for flavor, tags in [('suite', []), ('checked', ['-tags=wago_regalloccheck']),
                                 ('diagnostic', ['-tags=wago_codegenstats'])]:
                self.run('build-' + rev + '-' + flavor,
                         ['go', 'test', '-c', *tags, '-o', self.out / 'bin' / (rev + '-' + flavor), './suite'],
                         cwd=tree / 'bench', extra={'GOWORK': str(tree / 'go.work')})
            self.run('build-' + rev + '-runtime', ['go', 'build', '-trimpath', '-ldflags=-s -w',
                     '-o', self.out / 'bin' / (rev + '-runtime'), harness / 'runtime_memory.go'], cwd=tree)
            for flavor in ['suite', 'checked']:
                self.run('correctness-' + rev + '-' + flavor,
                         [self.out / 'bin' / (rev + '-' + flavor),
                          '-test.run=^Test(SharingFixtures|SharingZeroLocals|Corpus|CorpusSemanticExec)$',
                          '-test.v', '-test.count=1', '-wago.corpus=' + CORPUS], cwd=tree / 'bench/suite')
            for checked in [False, True]:
                tags = ['-tags=wago_regalloccheck'] if checked else []
                self.run('backend-check-' + rev + '-' + str(checked), ['go', 'test', '-p', '1',
                    '-count=1', *tags, './src/core/compiler/backend/railshot/shared',
                    './src/core/compiler/backend/railshot/' + native], cwd=tree)
                self.run('scalar-trap-check-' + rev + '-' + str(checked), ['go', 'test', '-p', '1',
                    '-count=1', '-v', *tags, '-run=SharedScalar|Scalar|TrapOrder', './src/wago'], cwd=tree)
            if self.args.full_tests:
                for checked in [False, True]:
                    # The named installer test intentionally installs the sibling
                    # workspace module. Other tests create standalone modules and
                    # need workspace discovery disabled for their child commands.
                    tags = ['-tags=wago_regalloccheck'] if checked else []
                    self.run('workspace-installer-test-' + rev + '-' + str(checked),
                        ['go', 'test', '-p', '1', '-count=1', '-v', *tags,
                         '-run=^TestGoInstallBuildsNamedInstallerCommand$', '.'], cwd=tree,
                        extra={'GOWORK': str(tree / 'go.work'), 'TERM': 'dumb'})
                    self.run('full-tests-' + rev + '-' + str(checked), ['go', 'test', '-p', '1',
                        '-count=1', '-v', *tags, '-skip=^TestGoInstallBuildsNamedInstallerCommand$', './...'], cwd=tree,
                        extra={'GOWORK': 'off', 'TERM': 'dumb'})
        self.manifest['binaries'] = {f.name: dict(sha256=sha(f), bytes=f.stat().st_size)
                                     for f in sorted((self.out / 'bin').iterdir())}
        # Module preparation may update dependency sums. Verify actual built
        # harness inputs still match, and save their post-preparation hashes.
        built_hashes = {rev: {relative: sha(tree / relative) for relative in hashes[rev]}
                        for rev, tree in self.trees.items()}
        if built_hashes['main'] != built_hashes['pr']:
            raise RuntimeError('Built harness/corpus hashes differ after setup')
        write_json(self.out / 'built-harness-corpus-sha256.json', built_hashes)
        self.manifest['status'] = 'prepared'
        write_json(self.out / 'manifest.json', self.manifest)

    def measure(self):
        cohorts = [
            ('compile', 'suite', ['-test.run=^$', '-test.bench=^(BenchmarkSharing(Native|Full|ZeroLocals)|BenchmarkCompile|BenchmarkCompileFull)$',
                                 '-test.benchtime=' + self.args.benchtime], {}),
            ('execution', 'suite', ['-test.run=^$', '-test.bench=^(BenchmarkSharingExec|BenchmarkExec)$',
                                   '-test.benchtime=' + self.args.exec_benchtime], {}),
            ('workers', 'suite', ['-test.run=^$', '-test.bench=^BenchmarkCompileFullWorkers$/(tiny|many_funcs|json-as)/(p1|auto)$',
                                 '-test.benchtime=' + self.args.benchtime], {'GOMAXPROCS': str(self.auto)}),
        ]
        for policy, extra in [('one', {}), ('auto', {'GOMAXPROCS': str(self.auto), 'WAGO_SHARING_WORKERS': '0'})]:
            for test, name in [('Memory', 'public'), ('MappedMemory', 'native')]:
                cohorts.append(('memory-' + name + '-' + policy, 'suite',
                    ['-test.run=^TestSharing' + test + '$', '-test.v'], dict(extra, WAGO_SHARING_MEMORY='1')))
            cohorts.append(('memory-runtime-' + policy, 'runtime', [], extra))
        self.manifest['status'] = 'measuring'
        write_json(self.out / 'manifest.json', self.manifest)
        print('Keep the Mac plugged in and idle. No builds/profiles occur during these runs.', flush=True)
        for kind, binary, flags, extra in cohorts:
            for block in range(1, self.args.blocks + 1):
                for seq, rev in enumerate(['main', 'pr', 'pr', 'main'], 1):
                    cmd = [self.out / 'bin' / (rev + '-' + binary), *flags]
                    if binary != 'runtime':
                        cmd += ['-test.count=1', '-test.timeout=30m', '-wago.corpus=' + CORPUS]
                    self.run(f'{kind}-b{block}-{seq}-{rev}', cmd,
                             cwd=self.trees[rev] / 'bench/suite', extra=extra,
                             kind=kind, revision=rev, block=block)
        # Instrumentation and heap profiles have their own processes after timing.
        for rev in self.trees:
            self.run('diagnostic-' + rev, [self.out / 'bin' / (rev + '-diagnostic'),
                     '-test.run=^TestSharingDiagnostic$', '-test.count=1', '-test.v', '-wago.corpus=' + CORPUS],
                     cwd=self.trees[rev] / 'bench/suite', kind='diagnostic', revision=rev)
            profiles = self.out / 'profiles' / rev
            profiles.mkdir()
            self.run('profiles-' + rev, [self.out / 'bin' / (rev + '-suite'),
                     '-test.run=^TestSharing(Memory|GCObservation)$', '-test.count=1', '-test.v'],
                     cwd=self.trees[rev] / 'bench/suite', kind='profile', revision=rev,
                     extra={'WAGO_SHARING_MEMORY': '1', 'WAGO_SHARING_PROFILE_DIR': str(profiles)})
            for phase in ['compile_release_270', 'released_2', 'runtime_released',
                          'engine_alive_gc0', 'engine_alive_gc3', 'engine_closed_extra_gc']:
                self.run('heap-top-' + rev + '-' + phase, ['go', 'tool', 'pprof', '-top', '-inuse_space',
                         self.out / 'bin' / (rev + '-suite'), profiles / (phase + '.heap')])
        self.run('power-after', ['pmset', '-g', 'custom'], optional=True)
        self.run('battery-after', ['pmset', '-g', 'batt'], optional=True)
        self.run('thermal-after', ['pmset', '-g', 'therm'], optional=True)

    def analyze(self):
        timing, memory, diagnostics = [], [], []
        aggregated = {rev: {kind: [] for kind in ['compile', 'execution', 'workers']} for rev in self.trees}
        signatures = {}
        for run in self.runs:
            rev, kind = run['revision'], run['kind']
            text = (self.out / run['log']).read_text()
            ident = dict(revision=rev, cohort=kind, process=run['name'], block=run['block'])
            if kind in ['compile', 'execution', 'workers']:
                rows = list(parse_benchmarks(text))
                names = {r['benchmark'] for r in rows}
                if not rows or len(names) != len(rows):
                    raise RuntimeError('Missing/duplicate benchmarks in ' + run['name'])
                if kind in signatures and names != signatures[kind]:
                    raise RuntimeError('Different benchmark row sets in ' + run['name'])
                signatures[kind] = names
                timing.extend(dict(ident, **r) for r in rows)
                aggregated[rev][kind].append(text)
                groups = {}
                for row in rows:
                    name = benchmark_name(row['benchmark'])
                    if name.startswith(('BenchmarkSharingNative/', 'BenchmarkCompile/')):
                        groups.setdefault('AllNative', []).append(row['ns/op'])
                    if name.startswith('BenchmarkSharingNative/') and name.split('/')[1] in SYNTHETIC:
                        groups.setdefault('MigratedSyntheticNative', []).append(row['ns/op'])
                    if name.startswith(('BenchmarkSharingFull/', 'BenchmarkCompileFull/')):
                        groups.setdefault('AllFull', []).append(row['ns/op'])
                    if name.startswith(('BenchmarkSharingExec/', 'BenchmarkExec/')):
                        groups.setdefault('AllExec', []).append(row['ns/op'])
                    if name.startswith('BenchmarkSharingExec/') and name.split('/')[1] in SYNTHETIC:
                        groups.setdefault('MigratedSyntheticExec', []).append(row['ns/op'])
                for group, values in groups.items():
                    value = math.exp(statistics.mean(math.log(x) for x in values))
                    line = f'BenchmarkAggregate/{group} 1 {value:.9f} ns/op\n'
                    aggregated[rev].setdefault('aggregates', []).append(line)
            if kind.startswith('memory-'):
                previous = None
                phases = []
                for data in extract_json(text):
                    if 'phase' not in data:
                        continue
                    phases.append(data['phase'])
                    expected_live = 36 if data['phase'].startswith('retained_') else 0
                    if data['modules'] != expected_live:
                        raise RuntimeError('Unexpected live output count in ' + run['name'])
                    row = dict(ident, **data)
                    native = row.pop('native', {})
                    row.update({'native_' + k: v for k, v in native.items()})
                    if previous:
                        row.update(allocation_bytes_delta=data['total_alloc'] - previous['total_alloc'],
                                   allocation_count_delta=data['mallocs'] - previous['mallocs'])
                    previous = data
                    memory.append(row)
                expected_end = 'released_2' if kind.startswith('memory-native-') else 'runtime_released'
                if previous is None or previous['phase'] != expected_end:
                    raise RuntimeError('Missing memory lifecycle in ' + run['name'])
                expected_phases = ['initial', 'compile_release_270', 'retained_0', 'released_0',
                                   'retained_1', 'released_1', 'retained_2', 'released_2']
                if expected_end == 'runtime_released':
                    expected_phases.append(expected_end)
                if phases != expected_phases:
                    raise RuntimeError('Unexpected memory lifecycle in ' + run['name'])
            if kind == 'diagnostic':
                for data in extract_json(text):
                    if 'stats' not in data:
                        continue
                    stats = data.pop('stats')
                    funcs = [f for f in (stats.get('Funcs') or []) if f]
                    row = dict(revision=rev, **data,
                        migrated_functions=sum(bool(f.get('SharedScalar')) for f in funcs),
                        migrated_body_bytes=sum(f.get('ScalarBodyBytes', 0) for f in funcs if f.get('SharedScalar')),
                        frame_bytes=sum(f.get('FrameBytes', 0) for f in funcs),
                        spill_slots=sum(f.get('MaxSpillSlots', 0) for f in funcs),
                        scalar_spills=sum(f.get('ScalarSpills', 0) for f in funcs),
                        scalar_reloads=sum(f.get('ScalarReloads', 0) for f in funcs),
                        admission_ns=sum(f.get('ScalarAdmissionNanos', 0) for f in funcs))
                    row.update({'compile_' + k: json.dumps(v) if isinstance(v, (list, dict)) else v
                                for k, v in stats.get('Compile', {}).items()})
                    row.update({'native_' + k: v for k, v in stats.get('NativeSize', {}).items()})
                    diagnostics.append(row)
        hashes = {rev: {r['name']: r['corpus_sha256'] for r in diagnostics if r['revision'] == rev} for rev in self.trees}
        if not hashes['main'] or hashes['main'] != hashes['pr']:
            raise RuntimeError('Diagnostic corpus differs between revisions')
        if not any(r['migrated_functions'] for r in diagnostics if r['revision'] == 'pr'):
            raise RuntimeError('PR did not exercise the shared compiler')
        for name in SYNTHETIC:
            row = next((r for r in diagnostics if r['revision'] == 'pr' and r['name'] == name), None)
            if row is None or row['migrated_functions'] != row['functions']:
                raise RuntimeError('Expected shared-path workload was not migrated: ' + name)
        write_csv(self.out / 'results/raw-timing.csv', timing)
        write_csv(self.out / 'results/raw-memory.csv', memory)
        write_csv(self.out / 'results/diagnostics.csv', diagnostics)
        write_csv(self.out / 'results/process-resources.csv', [r for r in self.runs if r['block']])
        for kind in ['compile', 'execution', 'workers', 'aggregates']:
            files = []
            for rev in self.trees:
                f = self.out / 'results' / (rev + '-' + kind + '.txt')
                f.write_text('\n'.join(aggregated[rev][kind]))
                files.append(f)
            for fmt in ['text', 'csv']:
                text = self.run('benchstat-' + kind + '-' + fmt,
                                [self.out / 'tools/benchstat', '-format=' + fmt, *files])
                (self.out / 'results' / ('comparison-' + kind + '.' + fmt)).write_text(text)
        rng = random.Random(802)
        groups = {}
        for row in timing:
            for metric in ['ns/op', 'B/op', 'allocs/op']:
                if metric in row:
                    # Worker CPU suffixes stay part of the benchmark name.
                    groups.setdefault((row['cohort'], row['benchmark'], metric), {}).setdefault(row['revision'], []).append(row[metric])
        summary = []
        for (cohort, name, metric), samples in sorted(groups.items()):
            a, b = samples['main'], samples['pr']
            am, bm = statistics.median(a), statistics.median(b)
            lo, hi = interval(a, b, rng) if min(a) > 0 else (None, None)
            change = 100 * (bm / am - 1) if am else None
            summary.append(dict(cohort=cohort, benchmark=name, metric=metric, main=am, pr=bm,
                                absolute_delta=bm-am, percent_delta=change, bootstrap_95_low=lo,
                                bootstrap_95_high=hi, main_processes=len(a), pr_processes=len(b),
                                main_min=min(a), main_max=max(a), pr_min=min(b), pr_max=max(b)))
        write_csv(self.out / 'results/summary.csv', summary)
        memgroups = {}
        for row in memory:
            for metric, value in row.items():
                if isinstance(value, (int, float)) and metric != 'block':
                    memgroups.setdefault((row['cohort'], row['phase'], metric), {}).setdefault(row['revision'], []).append(value)
        for run in self.runs:
            if run['kind'].startswith('memory-'):
                memgroups.setdefault((run['kind'], 'process', 'peak_rss_bytes'), {}).setdefault(run['revision'], []).append(run['peak_rss_bytes'])
        memsummary = []
        for (cohort, phase, metric), samples in sorted(memgroups.items()):
            a, b = samples['main'], samples['pr']
            am, bm = statistics.median(a), statistics.median(b)
            memsummary.append(dict(cohort=cohort, phase=phase, metric=metric, main=am, pr=bm,
                absolute_delta=bm-am, percent_delta=100*(bm/am-1) if am else None,
                main_processes=len(a), pr_processes=len(b), main_min=min(a), main_max=max(a), pr_min=min(b), pr_max=max(b)))
        write_csv(self.out / 'results/memory-summary.csv', memsummary)
        lines = ['# Wago macOS main / PR #802 comparison', '',
                 f"Main: `{self.manifest['commits']['main']}`. PR: `{self.manifest['commits']['pr']}`.", '',
                 f"Native {self.manifest['architecture']}; {self.args.blocks} ABBA blocks, {2*self.args.blocks} fresh processes per revision/cohort.", '',
                 'Each process contributes one sample per workload. All samples are retained. '
                 'See comparison-*.text for benchstat uncertainty and significance; summary.csv adds '
                 'seeded 10,000-resample independent-process median-ratio bootstrap 95% intervals. '
                 'Intervals overlapping zero are inconclusive, not evidence of equivalence.', '',
                 '## Key timing rows', '', '| Workload | Main ns/op | PR ns/op | Change | Bootstrap 95% |',
                 '|---|---:|---:|---:|---|']
        for row in summary:
            if row['metric'] == 'ns/op' and benchmark_name(row['benchmark']) in (
                    'BenchmarkSharingExec/join', 'BenchmarkExec/tiny.add', 'BenchmarkSharingZeroLocals/256'):
                lines.append(f"| {row['benchmark']} | {row['main']:.2f} | {row['pr']:.2f} | {row['percent_delta']:+.2f}% | [{row['bootstrap_95_low']:+.2f}%, {row['bootstrap_95_high']:+.2f}%] |")
        lines += ['', '## Aggregate timings (benchstat)', '', '```',
                  (self.out / 'results/comparison-aggregates.text').read_text().strip(), '```', '',
                  '## Memory (bytes, median [min, max])', '',
                  '| Scenario / phase / metric | Main | PR | Absolute change | Change |',
                  '|---|---:|---:|---:|---:|']
        for row in memsummary:
            if row['phase'] == 'process' or row['phase'] in ['retained_0', 'released_2', 'runtime_released'] and row['metric'] in ['heap_alloc', 'heap_inuse', 'rss_bytes', 'code_bytes', 'mapped_bytes']:
                percent = f"{row['percent_delta']:+.2f}%" if row['percent_delta'] is not None else 'n/a (zero baseline)'
                lines.append(f"| {row['cohort']} / {row['phase']} / {row['metric']} | {row['main']:g} [{row['main_min']:g}, {row['main_max']:g}] | {row['pr']:g} [{row['pr_min']:g}, {row['pr_max']:g}] | {row['absolute_delta']:+g} | {percent} |")
        lines += ['', '## Shared-path proof', '',
                  '| Revision | Functions | Migrated functions | Body bytes | Migrated body bytes |',
                  '|---|---:|---:|---:|---:|']
        for rev in self.trees:
            rows = [r for r in diagnostics if r['revision'] == rev]
            values = [sum(r[k] for r in rows) for k in ['functions', 'migrated_functions', 'body_bytes', 'migrated_body_bytes']]
            lines.append('| ' + rev + ' | ' + ' | '.join(str(v) for v in values) + ' |')
        lines += ['', '## Method and limitations', '',
            '- Native compile: decode/validation outside timing; backend analysis, code generation and explicit Close inside. '
            'Decoded input is reused using the existing harness. Full compile includes decode, validation, analysis, generation and Close.',
            '- Execution: compile/instantiate outside timing; verified fixtures/semantic oracles and fixed inputs. '
            'Synthetic calls verify results every iteration. Existing corpus checks outputs before timing; its call/error-check boundary is unchanged. '
            'JSON-AS retains mutable guest state between calls; it is not a strict reset-state execution comparison.',
            '- No compiled-output cache is used by these compile paths. Fixed-work memory tests compile/release 270 modules, '
            'then run three retain/release batches of 36 modules / 4128 functions. Runtime runner also retains 36 verified instances. '
            'GC is outside latency timing. No forced memory release or finalizer reliance.',
            '- Native mapping capacity is reported separately from rounded payload, generated code and Go allocation totals. '
            'Current RSS uses macOS ps (KiB converted to bytes). Darwin getrusage peak RSS is bytes. '
            'RSS sampling and Go test startup are included only in separate memory diagnostics; they can affect those observations.',
            '- Scratch peaks sum worker high-water counters: an accounting envelope, not simultaneous process RSS. '
            'Frame/spill sums are function storage totals, not simultaneously live native stack. Admission nanos are instrumented diagnostics, not production latency.',
            '- One-worker results use GOMAXPROCS=1. Normal-policy worker/memory results use the recorded machine logical CPU count '
            '(or --auto-procs) and adaptive worker count 0. Production builds have no diagnostic/check tags; checked and stats binaries run separately.',
            '- Compiler/corpus hashes and binary hashes are in the manifests. Benchmark code and fixture directories are copied '
            'from the frozen PR into main, identically hashed. The caller checkout is untouched. No mid-run rebase or ref update.',
            '- Default checks: synthetic/selected corpus semantic correctness, native backend tests, scalar/trap tests, '
            'all also with allocation checks. Full repository tests require --full-tests. External-tool/conformance skips remain in logs; '
            'missing spec submodules are not automatically fetched. This validates only this Mac architecture.',
            '- macOS has no taskset equivalent here. CPU/core placement, power and thermal noise remain possible. '
            'No power policy is changed; caffeinate inhibits idle sleep when available. '
            '--quick is a smoke run with too few processes for conclusions.', '',
            '## Potential timing regressions', '',
            'Rows above +5% median are listed for investigation; statistical uncertainty still applies. '
            'Use aggregate +2% and important-workload +5% as investigation thresholds, not acceptance criteria.', '']
        for row in sorted(summary, key=lambda r: r['percent_delta'] or 0, reverse=True):
            if row['metric'] == 'ns/op' and row['percent_delta'] > 5:
                lines.append(f"- {row['benchmark']}: {row['percent_delta']:+.2f}%, 95% [{row['bootstrap_95_low']:+.2f}%, {row['bootstrap_95_high']:+.2f}%].")
        (self.out / 'results/REPORT.md').write_text('\n'.join(lines) + '\n')
        self.manifest['status'] = 'complete'
        write_json(self.out / 'manifest.json', self.manifest)

    def archive(self):
        write_json(self.out / 'runs.json', self.runs)
        write_json(self.out / 'manifest.json', self.manifest)
        entries = ['run_me.sh', 'manifest.json', 'runs.json', 'logs', 'results', 'harness', 'profiles']
        if (self.out / 'failure.txt').exists():
            entries.append('failure.txt')
        entries += [p.name for p in self.out.glob('*sha256.json')]
        checksums = {str(p.relative_to(self.out)): sha(p) for entry in entries
                     for p in ([self.out / entry] if (self.out / entry).is_file() else (self.out / entry).rglob('*'))
                     if p.is_file()}
        write_json(self.out / 'evidence-sha256.json', checksums)
        entries.append('evidence-sha256.json')
        archive = self.out / 'results.tar.gz'
        with tarfile.open(archive, 'w:gz') as tar:
            for entry in entries:
                if (self.out / entry).exists():
                    tar.add(self.out / entry, arcname='wago-mac-results/' + entry)
        print('\nSend back: ' + str(archive), flush=True)

def main():
    parser = argparse.ArgumentParser(prog='run_me.sh', description='Compare origin/main with Wago PR #802 natively on macOS.',
        epilog='Keep the Mac plugged in and idle. Output includes REPORT.md, benchstat tables, raw CSV/logs, '
               'heap profiles, hashes, and results.tar.gz. Sources/binaries/cache stay in the output directory, '
               'outside the archive. No writes to your checkout and no PR changes.',
        formatter_class=argparse.ArgumentDefaultsHelpFormatter)
    parser.add_argument('--output', help='new results directory; default: unique directory under TMPDIR')
    parser.add_argument('--repo', help='fetch URL; default: this checkout origin')
    parser.add_argument('--main-ref', default='refs/heads/main', help='main ref or frozen baseline commit to fetch')
    parser.add_argument('--pr-ref', default='refs/pull/802/head', help='remote PR/branch ref or commit to fetch')
    parser.add_argument('--go-toolchain', default='go1.27.1', help='one pinned toolchain for both builds')
    parser.add_argument('--blocks', type=int, default=3, help='ABBA fresh-process blocks per cohort (minimum 3)')
    parser.add_argument('--benchtime', default='200ms', help='native/full compile and worker time per row')
    parser.add_argument('--exec-benchtime', default='1s', help='execution time per row')
    parser.add_argument('--auto-procs', type=int, help='normal-policy GOMAXPROCS; default: logical CPU count')
    parser.add_argument('--full-tests', action='store_true', help='also run full root tests, ordinary and checked, before timing')
    parser.add_argument('--prepare-only', action='store_true', help='fetch, build and check; omit measurements')
    parser.add_argument('--quick', action='store_true', help='smoke test only: one block, 100ms per row; inconclusive')
    args = parser.parse_args()
    if sys.version_info < (3, 9):
        parser.error('Python 3.9 or newer is required')
    if args.quick:
        args.blocks, args.benchtime, args.exec_benchtime = 1, '100ms', '100ms'
    elif args.blocks < 3:
        parser.error('use at least three complete blocks; --quick is available for smoke tests')
    if args.auto_procs is not None and args.auto_procs < 1:
        parser.error('--auto-procs must be positive')
    if not all(re.fullmatch(r'[0-9]+(?:\.[0-9]+)?(?:ms|s|m)', t) and float(re.match(r'[0-9.]+', t)[0]) > 0
               for t in [args.benchtime, args.exec_benchtime]):
        parser.error('benchmark times must be positive durations such as 200ms or 1s')
    if platform.system() != 'Darwin' or platform.machine() not in ['arm64', 'x86_64']:
        parser.error('run on a native Apple Silicon or Intel Mac')
    translated = subprocess.run(['sysctl', '-in', 'sysctl.proc_translated'], capture_output=True, text=True)
    if translated.stdout.strip() == '1':
        parser.error('Rosetta detected; use a native terminal and native Go/Python')
    for tool in ['git', 'go', 'gofmt']:
        if not shutil.which(tool):
            parser.error(tool + ' is required on PATH')
    experiment = Experiment(args)
    result = 0
    try:
        experiment.setup()
        if not args.prepare_only:
            experiment.measure()
            experiment.analyze()
            print('\nReport: ' + str(experiment.out / 'results/REPORT.md'), flush=True)
    except (Exception, KeyboardInterrupt) as error:
        experiment.manifest.update(status='failed', error=str(error))
        (experiment.out / 'failure.txt').write_text(traceback.format_exc())
        print('\nFAILED: ' + str(error), file=sys.stderr)
        result = 1
    finally:
        experiment.archive()
    return result

if __name__ == '__main__':
    sys.exit(main())
PY
