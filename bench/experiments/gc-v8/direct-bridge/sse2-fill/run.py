#!/usr/bin/env python3
import argparse
import hashlib
import json
import os
import pathlib
import platform
import subprocess
import time

p = argparse.ArgumentParser()
p.add_argument('--binary-dir', required=True)
p.add_argument('--output-dir', required=True)
p.add_argument('--node', default='node')
p.add_argument('--eden', type=int, default=524288)
p.add_argument('--audit', action='store_true')
p.add_argument('--matched', action='store_true', help='Short same-source baseline versus loop+copy comparison, not V8')
a = p.parse_args()
warmup = 128 if a.audit and a.eden >= 2097152 else 100
sample_count = 0 if a.audit else (1 if a.matched else 9)
batch = 5 if a.matched else 32
if a.matched:
    warmup = 128 if a.eden >= 2097152 else 100
root = pathlib.Path(__file__).resolve().parents[5]
base = pathlib.Path(a.binary_dir).resolve()
out = pathlib.Path(a.output_dir).resolve()
out.mkdir(parents=True, exist_ok=True)
corpus = root / 'bench/experiments/gc-v8'
manifest = {c['name']: c for c in json.loads((corpus / 'cases.json').read_text())}
cases = ['array-fill', 'array-allocation-access']
baseline_tags = 'wago_gc_direct_fill,wago_gc_direct_trusted_refstruct,wago_gc_direct_trusted_alloc,wago_gc_direct_trusted_loop,wago_gc_direct_copy'
variants = [('baseline', baseline_tags), ('candidate', baseline_tags + ',wago_gc_direct_fill_sse2'), ('v8', '--no-liftoff')]
def digest(path):
    h = hashlib.sha256()
    with path.open('rb') as f:
        for block in iter(lambda: f.read(262144), b''):
            h.update(block)
    return h.hexdigest()
provenance = {'source_commit': subprocess.check_output(['git', 'rev-parse', 'HEAD'], cwd=root, text=True).strip(), 'started_utc': time.strftime('%Y-%m-%dT%H:%M:%SZ', time.gmtime()), 'platform': platform.platform(), 'cpu': pathlib.Path('/proc/cpuinfo').read_text(), 'variants': {}, 'protocol': {'warmup': warmup, 'samples': sample_count, 'batch': batch, 'rounds': 1 if a.audit else 3}, 'eden_bytes': a.eden, 'old_semispace_bytes': 8 << 20, 'old_semispace_count': 2, 'allocation_elision': False, 'policy_sensitivity': a.eden != 524288, 'short_matched_comparison': a.matched, 'binary_source_note': 'Existing binary hashes identify the measured source; driver source may include reporting-only changes.'}
for label, tag in variants:
    if label != 'v8':
        binary = base / (label + '.test')
        provenance['variants'][label] = {'tag': tag, 'sha256': digest(binary), 'bytes': binary.stat().st_size}
    else:
        provenance['variants'][label] = {'flags': tag, 'versions': json.loads(subprocess.check_output([a.node, '-p', 'JSON.stringify(process.versions)'], text=True))}
rows = []
output = out / ('audit.json' if a.audit else 'results.json')
for round in range(1 if a.audit else 3):
    for case in cases[round % len(cases):] + cases[:round % len(cases)]:
        c = manifest[case]
        for label, tag in variants[round % len(variants):] + variants[:round % len(variants)]:
            env = dict(os.environ, GOMAXPROCS='1')
            if label == 'v8':
                cmd = [a.node, '--no-liftoff', str(pathlib.Path(__file__).with_name('v8.cjs')), str(corpus / c['file']), c['export'], str(c['n']), str(c['want']), c['sha256'], str(warmup), str(sample_count), str(batch)]
            else:
                env.update(GC_BRIDGE_CASE=case, GC_BRIDGE_MODE='direct', GC_BRIDGE_COMPILER_MODE=label, GC_BRIDGE_EDEN=str(a.eden), GC_BRIDGE_WARMUP=str(warmup), GC_BRIDGE_SAMPLES=str(sample_count), GC_BRIDGE_BATCH=str(batch))
                cmd = [str(base / (label + '.test')), '-test.run=^TestGCDirectBridgeProbe$', '-test.v', '-test.count=1']
            r = subprocess.run(cmd, cwd=root / 'src/wago', env=env, text=True, capture_output=True)
            row = {'round': round, 'case': case, 'variant': label, 'returncode': r.returncode, 'stdout': r.stdout, 'stderr': r.stderr}
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
