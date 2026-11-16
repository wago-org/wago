#!/usr/bin/env python3
import argparse
import hashlib
import json
import os
import pathlib
import platform
import subprocess
import time
import datetime

p = argparse.ArgumentParser()
p.add_argument('--binary-dir', required=True)
p.add_argument('--output-dir', required=True)
p.add_argument('--node', default='node')
p.add_argument('--eden', type=int, default=2097152)
p.add_argument('--audit', action='store_true')
a = p.parse_args()
warmup = 128 if a.audit and a.eden >= 2097152 else 100
root = pathlib.Path('wago-gc-marks-baseline')
base = pathlib.Path(a.binary_dir).resolve()
out = pathlib.Path(a.output_dir).resolve()
out.mkdir(parents=True, exist_ok=True)
corpus = root / 'bench/experiments/gc-v8'
manifest = {c['name']: c for c in json.loads((corpus / 'cases.json').read_text())}
cases = ['struct-allocation-access', 'array-allocation-access', 'array-fill', 'array-copy-checksum', 'array-loop-copy-checksum', 'tree-survival']
variants = [('control', 'wago_gc_direct_trusted_loop,wago_gc_direct_trusted_refstruct,wago_gc_direct_trusted_alloc,wago_gc_direct_fill_sse2,wago_gc_direct_copy,wago_gc_direct_array_alloc,wago_gc_direct_weighted_sum'), ('candidate', 'wago_gc_direct_trusted_loop,wago_gc_direct_trusted_refstruct,wago_gc_direct_trusted_alloc,wago_gc_direct_fill_sse2,wago_gc_direct_copy,wago_gc_direct_array_alloc,wago_gc_direct_weighted_sum,wago_gc_direct_arena_cache'), ('v8', '--no-liftoff')]
def digest(path):
    h = hashlib.sha256()
    with path.open('rb') as f:
        for block in iter(lambda: f.read(262144), b''):
            h.update(block)
    return h.hexdigest()
provenance = {'source_commit': subprocess.check_output(['git', 'rev-parse', 'HEAD'], cwd=root, text=True).strip(), 'started_utc': time.strftime('%Y-%m-%dT%H:%M:%SZ', time.gmtime()), 'platform': platform.platform(), 'cpu': pathlib.Path('/proc/cpuinfo').read_text(), 'variants': {}, 'protocol': {'warmup': warmup, 'samples': 0 if a.audit else 9, 'batch': 32, 'rounds': 1 if a.audit else 3}, 'total_young_budget_bytes': a.eden, 'old_semispace_bytes': 8 << 20, 'old_semispace_count': 2, 'allocation_elision': False, 'age2_enabled': False, 'candidate_qualification': 'Require final qualified source and CPU release before running', 'policy_sensitivity': a.eden != 2097152, 'binary_source_note': 'Existing binary hashes identify the measured source; driver source may include reporting-only changes.'}
if a.audit:
    variants = [v for v in variants if v[0] != 'v8']
for label, tag in variants:
    if label != 'v8':
        binary = base / (label + '.test')
        provenance['variants'][label] = {'tag': tag, 'sha256': digest(binary), 'bytes': binary.stat().st_size}
    else:
        provenance['variants'][label] = {'flags': tag, 'versions': json.loads(subprocess.check_output([a.node, '-p', 'JSON.stringify(process.versions)'], text=True))}
rows = []
output = out / ('audit.json' if a.audit else 'results.json')
for round in range(1 if a.audit else 3):
    for case in cases[round:] + cases[:round]:
        c = manifest[case]
        for label, tag in variants[round % len(variants):] + variants[:round % len(variants)]:
            env = dict(os.environ, GOMAXPROCS='1')
            if label == 'v8':
                cmd = [a.node, '--no-liftoff', str(pathlib.Path(__file__).with_name('v8.cjs')), str(corpus / c['file']), c['export'], str(c['n']), str(c['want']), c['sha256'], str(warmup), '0' if a.audit else '9', '32']
            else:
                env.update(GC_BRIDGE_AGE2='0', GC_BRIDGE_CASE=case, GC_BRIDGE_MODE='direct', GC_BRIDGE_COMPILER_MODE=label, GC_BRIDGE_EDEN=str(a.eden), GC_BRIDGE_WARMUP=str(warmup), GC_BRIDGE_SAMPLES='0' if a.audit else '9', GC_BRIDGE_BATCH='32')
                cmd = [str(base / (label + '.test')), '-test.run=^TestGCDirectBridgeProbe$', '-test.v', '-test.count=1']
            started_utc = datetime.datetime.now(datetime.timezone.utc).isoformat()
            load_before = os.getloadavg()
            started_monotonic = time.monotonic()
            r = subprocess.run(cmd, cwd=root / 'src/wago', env=env, text=True, capture_output=True)
            row = {'started_utc': started_utc, 'ended_utc': datetime.datetime.now(datetime.timezone.utc).isoformat(), 'wall_seconds': time.monotonic()-started_monotonic, 'load_before': load_before, 'load_after': os.getloadavg(), 'allowed_affinity': sorted(os.sched_getaffinity(0)), 'round': round, 'case': case, 'variant': label, 'returncode': r.returncode, 'stdout': r.stdout, 'stderr': r.stderr}
            for line in r.stdout.splitlines():
                if 'DIRECT_BRIDGE_RESULT ' in line:
                    row['result'] = json.loads(line.split('DIRECT_BRIDGE_RESULT ', 1)[1])
                elif label == 'v8' and line.startswith('{'):
                    row['result'] = json.loads(line)
            rows.append(row)
            output.write_text(json.dumps({'provenance': provenance, 'rows': rows}, indent=2))
            print(round, case, label, r.returncode, flush=True)
            if r.returncode or 'result' not in row:
                raise SystemExit('Probe failed; raw evidence retained')
            if label != 'v8':
                observed = row['result']
                if observed.get('mode') != 'direct' or not observed.get('native_direct_view_installed'):
                    raise SystemExit('Direct receipt/view audit failed; raw evidence retained')
                if observed['config']['ExperimentalCompactOffsets'] or observed['config']['ExperimentalCompactAge2']:
                    raise SystemExit('Unexpected compact/age2 configuration; raw evidence retained')
