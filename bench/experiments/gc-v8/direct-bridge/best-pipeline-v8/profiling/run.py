#!/usr/bin/env python3
"""Separate CPU sampling experiment; not benchmark timing evidence."""
import argparse
import hashlib
import json
import os
import pathlib
import subprocess
import time

p = argparse.ArgumentParser()
p.add_argument('--binary', required=True)
p.add_argument('--output-dir', required=True)
p.add_argument('--go', default='go')
a = p.parse_args()
root = pathlib.Path(__file__).resolve().parents[6]
binary = pathlib.Path(a.binary).resolve()
out = pathlib.Path(a.output_dir).resolve()
out.mkdir(parents=True, exist_ok=True)
h = hashlib.sha256()
with binary.open('rb') as f:
    for block in iter(lambda: f.read(262144), b''):
        h.update(block)
original = json.loads((pathlib.Path(__file__).resolve().parents[1] / 'evidence/primary/results.json').read_text())['provenance']
if h.hexdigest() != original['variants']['candidate']['sha256']:
    raise SystemExit('Expected unchanged original ABI3 best-pipeline binary')
records = {'measured_binary_source': original['source_commit'], 'binary_sha256': h.hexdigest(), 'started_utc': time.strftime('%Y-%m-%dT%H:%M:%SZ', time.gmtime()), 'mode': 'untimed CPU sampling, not benchmark evidence', 'eden_bytes': 2097152, 'old_semispace_bytes': 8 << 20, 'rows': []}
for case in ['struct-allocation-access', 'array-copy-checksum']:
    profile = out / (case + '.cpu.pprof')
    env = dict(os.environ, GC_BRIDGE_CASE=case, GC_BRIDGE_MODE='direct', GC_BRIDGE_COMPILER_MODE='profile_abi3', GC_BRIDGE_EDEN='2097152', GC_BRIDGE_WARMUP='128', GC_BRIDGE_SAMPLES='1', GC_BRIDGE_BATCH='512', GOMAXPROCS='1')
    r = subprocess.run([str(binary), '-test.run=^TestGCDirectBridgeProbe$', '-test.v', '-test.count=1', '-test.cpuprofile=' + str(profile)], cwd=root / 'src/wago', env=env, text=True, capture_output=True)
    record = {'case': case, 'returncode': r.returncode, 'stdout': r.stdout, 'stderr': r.stderr}
    records['rows'].append(record)
    (out / 'profile-run.json').write_text(json.dumps(records, indent=2))
    if r.returncode:
        raise SystemExit('Profile probe failed; raw evidence retained')
    for mode in ['flat', 'cum']:
        cmd = [a.go, 'tool', 'pprof', '-top', '-nodecount=40']
        if mode == 'cum':
            cmd.append('-cum')
        report = subprocess.run(cmd + [str(binary), str(profile)], text=True, capture_output=True)
        (out / (case + '.' + mode + '.txt')).write_text(report.stdout + report.stderr)
        if report.returncode:
            raise SystemExit('pprof report failed; raw profile retained')
    print(case, 'profile completed', flush=True)
