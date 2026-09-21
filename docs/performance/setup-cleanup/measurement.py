"""Shared validation for isolated setup/cleanup measurements (standard library only)."""
import hashlib
import json
import math
import os
from pathlib import Path
import re
import subprocess
import time
import uuid

ROOT = Path(__file__).resolve().parents[3]


def digest(path):
    return hashlib.sha256(Path(path).read_bytes()).hexdigest()


def capture(cmd, cwd=ROOT):
    return subprocess.check_output(cmd, cwd=cwd, text=True).strip()


def fresh_directory(path):
    path = Path(path).resolve()
    path.mkdir(parents=True, exist_ok=True)
    if any(path.iterdir()):
        raise ValueError(f'output directory must be empty: {path}')
    return path


def executable(path):
    path = Path(path).resolve()
    if not path.is_file() or not os.access(path, os.X_OK):
        raise ValueError(f'missing executable: {path}')
    return path


def rows(output):
    result = {}
    if not re.search(r'^PASS$', output, re.M) or re.search(r'^(FAIL|--- FAIL:)', output, re.M):
        raise ValueError('missing PASS or failed test output')
    for line in output.splitlines():
        fields = line.split()
        if not fields or not fields[0].startswith('Benchmark') or len(fields) < 2 or not fields[1].isdigit():
            continue
        name = re.sub(r'-\d+$', '', fields[0])
        if int(fields[1]) <= 0 or len(fields[2:]) % 2:
            raise ValueError(f'invalid benchmark row: {line}')
        metrics = {}
        for value, unit in zip(fields[2::2], fields[3::2]):
            value = float(value)
            if not math.isfinite(value) or value < 0 or unit in metrics:
                raise ValueError(f'invalid metric: {line}')
            metrics[unit] = value
        if not {'ns/op', 'B/op', 'allocs/op'} <= metrics.keys() or name in result:
            raise ValueError(f'missing metrics or duplicate leaf: {line}')
        result[name] = metrics
    if not result:
        raise ValueError('filter produced no valid benchmark result')
    return result


def settings(gomaxprocs, cpus):
    if gomaxprocs < 1:
        raise ValueError('GOMAXPROCS must be positive')
    env = dict(os.environ, GOMAXPROCS=str(gomaxprocs), GOGC='100', GOMEMLIMIT='off', GODEBUG='', WAGO_BOUNDS='signals')
    prefix = []
    if cpus:
        prefix = ['taskset', '-c', cpus]
        subprocess.run(prefix + ['true'], check=True)
    return env, prefix


def host():
    data = {}
    for name in ['/proc/loadavg', '/proc/pressure/memory', '/proc/meminfo']:
        try:
            data[name] = Path(name).read_text()
        except OSError:
            pass
    data['temperature'] = {str(p): p.read_text() for p in Path('/sys/class/thermal').glob('thermal_zone*/temp')}
    return data


def identity(env, binaries):
    return dict(run_id=str(uuid.uuid4()), created_utc=time.strftime('%Y-%m-%dT%H:%M:%SZ', time.gmtime()),
                source_commit=capture(['git', 'rev-parse', 'HEAD']),
                source_status=capture(['git', 'status', '--short']),
                source_diff_sha256=hashlib.sha256(capture(['git', 'diff', 'HEAD']).encode()).hexdigest(),
                toolchain=capture(['go', 'version']), build_environment=json.loads(capture(['go', 'env', '-json'])),
                machine=capture(['uname', '-a']), affinity=sorted(os.sched_getaffinity(0)) if hasattr(os, 'sched_getaffinity') else None,
                environment={k: env.get(k) for k in ['GOMAXPROCS', 'GOGC', 'GOMEMLIMIT', 'GODEBUG', 'WAGO_BOUNDS']},
                binaries={k: {'path': str(v), 'sha256': digest(v)} for k, v in binaries.items()})


def run_process(command, path, env, timeout=300):
    started = time.monotonic()
    before = host()
    with Path(path).open('x') as f:
        process = subprocess.run(command, cwd=ROOT/'bench/suite', env=env, stdout=f, stderr=subprocess.STDOUT, timeout=timeout)
    if process.returncode:
        raise ValueError(f'benchmark failed ({process.returncode}); see {path}')
    return dict(command=command, seconds=time.monotonic()-started, output=Path(path).name,
                output_sha256=digest(path), host_before=before, host_after=host())
