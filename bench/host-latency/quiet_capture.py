#!/usr/bin/env python3
"""Guarded paired latency capture; never publish partial/busy-host summaries."""
import argparse
import csv
import hashlib
import json
import math
import os
from pathlib import Path
import shutil
import subprocess
import time


def parse_processes(output):
    rows = []
    for line in output.splitlines()[1:]:
        parts = line.strip().split(None, 3)
        if len(parts) != 4:
            continue
        try:
            rows.append((int(parts[0]), int(parts[1]), float(parts[2]), parts[3]))
        except ValueError:
            continue
    return rows


def blockers(rows, owner, paused_queue=None):
    owned = {owner}
    while True:
        expanded = owned | {pid for pid, parent, _, _ in rows if parent in owned}
        if expanded == owned:
            break
        owned = expanded
    result = []
    for pid, parent, cpu, command in rows:
        if pid in owned:
            continue
        executable = Path(command.split()[0]).name
        collector = ('.wasmbench/' in command or '/.cache/wasm-fyi/' in command) and (
            executable in {'controller', 'node', 'd8', 'wasm-analyze'} or executable.startswith('adapter-'))
        build = executable in {'rustc', 'clang', 'clang++', 'cc1', 'make', 'ninja'} or (
            executable == 'go' and any(word in command.split()[1:] for word in ['build', 'test']))
        if (collector and pid != paused_queue) or build or cpu >= 50:
            result.append({'pid': pid, 'ppid': parent, 'cpu_percent': cpu, 'command': command})
    return result


def verified_paused_queue(site, rows):
    if site is None:
        return None
    try:
        control = site / '.wasmbench/history-background'
        receipt = json.loads((control / 'paused-hub.json').read_text())
        request = json.loads((control / 'pause-after-week.json').read_text())
        finish = request['machines']['hub']['finishWeek']
        if receipt['status'] != 'paused' or receipt['finishWeek'] != finish or receipt['blockedWeek'] == finish or receipt['requestedAt'] != request['requestedAt']:
            return None
        pid = receipt['pid']
        row = next(row for row in rows if row[0] == pid)
        argv = row[3].split()
        if len(argv) != 4 or Path(argv[0]).name != 'node' or argv[1] != 'scripts/weekly-queue.mjs' or Path(argv[3]).name != 'weekly-' + receipt['blockedWeek'].replace('-', ''):
            return None
        if Path('/proc', str(pid), 'cwd').resolve() != site.resolve() or any(parent == pid for _, parent, _, _ in rows):
            return None
        # Pin the audited queue and pause loop. A changed supervisor must be
        # re-audited; an absent/resumed request immediately loses this exception.
        pins = {'scripts/weekly-queue.mjs': '6b4ce2c75bb111624922f4471ae43c8d108a5b8ad2808b83c1bddb5570b4fb40',
                'scripts/lib/weekly-pause.mjs': '2dad6fe9f369ec72caf19d3740f532c5e099051eb0cacfb09014a4ec303506b7'}
        if any(digest(site / name) != expected for name, expected in pins.items()):
            return None
        return {'pid': pid, 'receipt': receipt, 'request': request, 'source_pins': pins}
    except (OSError, ValueError, KeyError, StopIteration, TypeError):
        return None


def snapshot(paused_weekly_site=None):
    output = subprocess.check_output(['ps', '-eo', 'pid,ppid,pcpu,args'], text=True)
    rows = parse_processes(output)
    paused = verified_paused_queue(paused_weekly_site, rows)
    state = {'paused_queue': paused, 'time_ns': time.time_ns(), 'uptime': subprocess.check_output(['uptime'], text=True).strip(),
             'processes': output, 'blockers': blockers(rows, os.getpid(), paused['pid'] if paused else None)}
    # Preserve Linux per-CPU scheduling and hypervisor steal counters. A quiet
    # process list alone cannot establish an idle physical host for a VM.
    if Path('/proc/stat').is_file():
        state['linux_cpu_ticks'] = {row.split()[0]: list(map(int, row.split()[1:]))
                                   for row in Path('/proc/stat').read_text().splitlines()
                                   if row.startswith('cpu')}
    if Path('/proc/self/personality').is_file():
        state['linux_personality'] = Path('/proc/self/personality').read_text().strip()
    if hasattr(os, 'sched_getaffinity'):
        state['cpu_affinity'] = sorted(os.sched_getaffinity(0))
    return state


def digest(path):
    return hashlib.sha256(Path(path).read_bytes()).hexdigest()


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--baseline', type=Path, required=True)
    parser.add_argument('--candidate', type=Path, required=True)
    parser.add_argument('--reference', type=Path, required=True)
    parser.add_argument('--fixture', type=Path, required=True)
    parser.add_argument('--out', type=Path, required=True)
    parser.add_argument('--paused-weekly-site', type=Path, help='allow only an audited queue proven waiting on the live Hub pause request')
    parser.add_argument('--quiet-seconds', type=float, default=10)
    parser.add_argument('--work', type=int, default=4194304)
    parser.add_argument('--batch-min', type=int, default=64)
    parser.add_argument('--blocks', type=int, default=4)
    parser.add_argument('--pair-by-api', action='store_true',
                        help='run each baseline/candidate API pair consecutively')
    args = parser.parse_args()
    if min(args.work, args.batch_min, args.blocks) <= 0:
        parser.error('work, batch-min and blocks must be positive')
    if args.blocks != 4 and not args.pair_by_api:
        parser.error('non-default blocks requires pair-by-api')
    if args.quiet_seconds < 0:
        parser.error('quiet-seconds must be nonnegative')
    wat = args.fixture.with_suffix(".wat")
    for path in [args.baseline, args.candidate, args.reference, args.fixture, wat]:
        if not path.is_file():
            parser.error(f'missing input: {path}')
    args.out.mkdir(parents=True, exist_ok=False)
    evidence = {'status': 'preflight', 'snapshots': [], 'snapshot_log': 'host-snapshots.jsonl', 'commands': [],
                'inputs': {str(path.resolve()): digest(path) for path in
                           [args.baseline, args.candidate, args.reference, args.fixture, wat]}}
    def save():
        (args.out / 'capture.json').write_text(json.dumps(evidence, indent=2))
    def check():
        state = snapshot(args.paused_weekly_site)
        with (args.out / 'host-snapshots.jsonl').open('a') as log:
            log.write(json.dumps(state) + '\n')
        evidence['snapshots'].append({key: state[key] for key in ['time_ns', 'uptime', 'blockers']})
        save()
        return not state['blockers']
    deadline = time.monotonic() + args.quiet_seconds
    while True:
        if not check():
            evidence['status'] = 'blocked-before-timing'
            save()
            print('Host is busy; no timing launched. See capture.json.')
            return 75
        if time.monotonic() >= deadline:
            break
        time.sleep(min(0.5, max(0, deadline - time.monotonic())))
    fixture_dir = args.out / 'fixture'
    fixture_dir.mkdir()
    shutil.copyfile(args.fixture, fixture_dir / 'yield.wasm')
    shutil.copyfile(wat, fixture_dir / 'yield.wat')
    plan = []
    for phase in ['reference-before', 'baseline-1', 'candidate-1', 'candidate-2', 'baseline-2',
                  'candidate-3', 'baseline-3', 'baseline-4', 'candidate-4', 'reference-after']:
        if phase.startswith('reference'):
            plan.append((phase, [str(args.reference.resolve()), '--yield-atomic', '1', '65536']))
        else:
            binary = args.baseline if phase.startswith('baseline') else args.candidate
            apis = ['session', 'prepared', 'instance'] if phase.endswith(('3', '4')) else ['instance', 'prepared', 'session']
            for api in apis:
                plan.append((phase + '-' + api, [str(binary.resolve()), '--api=' + api,
                    f'--work={args.work}', f'--batch-min={args.batch_min}', '--yield-atomic', '1', '65536']))
    if args.pair_by_api:
        plan = [('reference-before', [str(args.reference.resolve()), '--yield-atomic', '1', '65536'])]
        for block in range(1, args.blocks + 1):
            apis = ['instance', 'prepared', 'session'] if block <= (args.blocks + 1) // 2 else ['session', 'prepared', 'instance']
            owners = ['baseline', 'candidate'] if block % 2 else ['candidate', 'baseline']
            for api in apis:
                for owner in owners:
                    binary = args.baseline if owner == 'baseline' else args.candidate
                    name = f'{owner}-{block}-{api}'
                    plan.append((name, [str(binary.resolve()), '--api=' + api,
                        f'--work={args.work}', f'--batch-min={args.batch_min}', '--yield-atomic', '1', '65536']))
        plan.append(('reference-after', [str(args.reference.resolve()), '--yield-atomic', '1', '65536']))
    evidence['blocks'] = args.blocks
    evidence['work'] = args.work
    evidence['batch_min'] = args.batch_min
    evidence['pair_by_api'] = args.pair_by_api
    evidence['status'] = 'running'
    environment = {**os.environ, 'WAGO_ARM64_INTEGER_HOST': '1', 'GOMAXPROCS': '1'}
    for name, command in plan:
        print(f'Starting {name}', flush=True)
        if not check():
            evidence['status'] = 'interference-before-command'
            save()
            return 75
        before_fixture = digest(fixture_dir / 'yield.wasm')
        evidence['commands'].append({'name': name, 'argv': command, 'env': {'WAGO_ARM64_INTEGER_HOST': '1', 'GOMAXPROCS': '1'},
                                    'fixture_before': before_fixture})
        save()
        with (args.out / (name + '.csv')).open('w') as output, (args.out / (name + '.stderr')).open('w') as errors:
            child = subprocess.Popen(command, cwd=fixture_dir, env=environment, stdout=output, stderr=errors)
            while child.poll() is None:
                if not check():
                    child.terminate()
                    try:
                        child.wait(timeout=5)
                    except subprocess.TimeoutExpired:
                        child.kill()
                        child.wait()
                    evidence['status'] = 'interference-during-command'
                    evidence['commands'][-1]['returncode'] = child.returncode
                    save()
                    return 75
                time.sleep(0.5)
        evidence['commands'][-1]['returncode'] = child.returncode
        if child.returncode != 0:
            evidence['status'] = 'command-failed'
            save()
            return 1
        if digest(fixture_dir / 'yield.wasm') != before_fixture:
            evidence['status'] = 'fixture-changed'
            save()
            return 1
        with (args.out / (name + '.csv')).open() as output:
            rows = list(csv.reader(output))
        groups = {}
        valid = bool(rows)
        try:
            for row in rows:
                valid = valid and len(row) == 6 and int(row[2]) in [1, 65536] and int(row[3]) == 1 and math.isfinite(float(row[-1])) and float(row[-1]) > 0
                groups.setdefault((row[1], int(row[2])), []).append(int(row[4]))
            valid = valid and len(groups) == (4 if name.startswith('reference') else 6) and all(sorted(samples) == list(range(5)) for samples in groups.values())
        except (ValueError, IndexError):
            valid = False
        if not valid:
            evidence['status'] = 'invalid-output'
            save()
            return 1
        print(f'Completed {name}', flush=True)
    if not check():
        evidence['status'] = 'interference-after-timing'
        save()
        return 75
    evidence['status'] = 'complete'
    evidence['files'] = {file.name: digest(file) for file in args.out.glob('*.csv')}
    save()
    print('Guarded capture complete; raw results and host snapshots retained.')
    return 0


if __name__ == '__main__':
    raise SystemExit(main())
