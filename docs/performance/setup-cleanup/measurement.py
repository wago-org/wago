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
                script_hashes={str(p.relative_to(ROOT)):digest(p) for p in sorted((ROOT/'docs/performance/setup-cleanup').rglob('*.py'))},
                source_commit=capture(['git', 'rev-parse', 'HEAD']),
                source_status=capture(['git', 'status', '--short']),
                source_diff_sha256=hashlib.sha256(capture(['git', 'diff', 'HEAD']).encode()).hexdigest(),
                toolchain=capture(['go', 'version']), build_environment=json.loads(capture(['go', 'env', '-json'])),
                machine=capture(['uname', '-a']), affinity=sorted(os.sched_getaffinity(0)) if hasattr(os, 'sched_getaffinity') else None,
                environment={k: env.get(k) for k in ['GOMAXPROCS', 'GOGC', 'GOMEMLIMIT', 'GODEBUG', 'WAGO_BOUNDS']},
                binaries={k: {'path': str(v), 'sha256': digest(v)} for k, v in binaries.items()})


class TemperatureGuard:
    def __init__(self, path, maximum, start, stable_seconds, wait_limit, log):
        if not 0 < start < maximum or stable_seconds < 0 or wait_limit <= stable_seconds:
            raise ValueError('invalid temperature limits')
        self.path = Path(path)
        self.maximum, self.start = maximum, start
        self.stable_seconds, self.wait_limit = stable_seconds, wait_limit
        self.log = log

    def read(self, phase):
        value = float(self.path.read_text())/1000
        if not math.isfinite(value) or not 0 < value < 150:
            raise ValueError('invalid CPU temperature reading')
        self.log.write(json.dumps(dict(time=time.time(), phase=phase, cpu_celsius=value))+'\n')
        self.log.flush()
        return value

    def wait(self):
        began = time.monotonic()
        stable = None
        while True:
            now = time.monotonic()
            value = self.read('cooling')
            if value <= self.start:
                if stable is None:
                    stable = now
                if now-stable >= self.stable_seconds:
                    return now-began
            else:
                stable = None
            if now-began >= self.wait_limit:
                raise ValueError('CPU did not cool within the time limit')
            time.sleep(.25)

    def check(self):
        value = self.read('benchmark')
        if value >= self.maximum:
            raise ValueError(f'CPU temperature stop: {value:g} C >= {self.maximum:g} C')
        return value


def cpu_temperature_sensor():
    for directory in sorted(Path('/sys/class/hwmon').glob('hwmon*')):
        if (directory/'name').read_text().strip() not in ['k10temp', 'coretemp', 'zenpower']:
            continue
        for sensor in sorted(directory.glob('temp*_input')):
            label = sensor.with_name(sensor.name.replace('_input', '_label'))
            if label.exists() and label.read_text().strip() in ['Tctl', 'Package id 0']:
                return sensor
    raise ValueError('no supported CPU temperature sensor; no benchmark started')


def run_process(command, path, env, timeout=300, thermal=None):
    cooling_seconds = thermal.wait() if thermal else 0
    peak_temperature = thermal.check() if thermal else None
    started = time.monotonic()
    before = host()
    usage = None
    sampled_peak = 0
    exec_hwm = 0
    process = None
    with Path(path).open('x') as f:
        try:
            process = subprocess.Popen(command, cwd=ROOT/'bench/suite', env=env, stdout=f, stderr=subprocess.STDOUT)
            while True:
                pid, status, current = os.wait4(process.pid, os.WNOHANG)
                if pid:
                    process.returncode = os.waitstatus_to_exitcode(status)
                    usage = current
                    break
                if thermal:
                    peak_temperature = max(peak_temperature, thermal.check())
                if time.monotonic()-started > timeout:
                    raise ValueError(f'benchmark exceeded {timeout}s limit: {path}')
                try:
                    status_text = Path(f'/proc/{process.pid}/status').read_text()
                    highwater = re.search(r'^VmHWM:\s+(\d+)', status_text, re.M)
                    if highwater:
                        exec_hwm = max(exec_hwm, int(highwater[1]))
                    match = re.search(r'^VmRSS:\s+(\d+)', status_text, re.M)
                    if match:
                        rss = int(match[1])
                        sampled_peak = max(sampled_peak, rss)
                        if rss > 4*1024*1024:
                            raise ValueError(f'benchmark exceeded 4 GiB RSS limit: {path}')
                except FileNotFoundError:
                    pass
                time.sleep(.05)
        finally:
            if process is not None and process.returncode is None:
                process.kill()
                process.wait()
    if process.returncode:
        raise ValueError(f'benchmark failed ({process.returncode}); see {path}')
    return dict(command=command, seconds=time.monotonic()-started, output=Path(path).name,
                output_sha256=digest(path), host_before=before, host_after=host(),
                whole_process_max_rss_kib=usage.ru_maxrss, peak_scope="fork-to-exit",
                postexec_hwm_kib=exec_hwm or None, sampled_peak_rss_kib=sampled_peak,
                minor_faults=usage.ru_minflt, major_faults=usage.ru_majflt,
                user_seconds=usage.ru_utime, system_seconds=usage.ru_stime,
                sample_interval_seconds=.05, rss_limit_kib=4*1024*1024,
                cooling_seconds=cooling_seconds, maximum_sampled_cpu_celsius=peak_temperature)
