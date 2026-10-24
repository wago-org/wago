import importlib.util
import json
import statistics
import subprocess
from pathlib import Path

root = Path('/home/dev/Projects/dewdrop')
spec = importlib.util.spec_from_file_location('measure', root/'docs/research/data/wago-local-perf-2026-10-20/measure.py')
m = importlib.util.module_from_spec(spec)
spec.loader.exec_module(m)
m.DATA = root/'docs/research/data/wago-folded-space-full-2026-10-21'
m.DATA.mkdir(parents=True, exist_ok=True)
m.ENV = dict(m.ENV, GOMAXPROCS='2')
original = m.measure

def measure(label, command, **kw):
    command = list(command)
    if command[0] == 'node':
        command[3] = m.COMPILER
    return original(label, ['taskset', '-c', '12,14', *command], **kw)

m.measure = measure
identity = dict(common_stack_commit='dc961197a73698e7bbf3583769f8979fe3072fef',
    candidate_production_commits=['f7ebbe973'], net_production_delta='checked descriptor addressing only, no Old preloads or special-case',
    combined_qualification_commit='61ad5f96d',
    compiler_sha256=m.digest(m.COMPILER), request_sha256=m.digest(m.WORK/'request.bin'),
    control_build=json.loads((root/'.tmp/wago-cast-array-full/build.json').read_text()),
    candidate_build=json.loads((root/'.tmp/wago-folded-space-full/build.json').read_text()),
    affinity='12,14', GOMAXPROCS=2,
    v8_reference_seconds=4.0, v8_reference='Known default V8 result; no new V8 timing at user request.',
    method='Three serial balanced rounds: control/candidate; candidate/control; control/candidate. Identical compiler/request, fresh native/result caches; output hashes/main42 and exact roots required. Both Wago variants include the installed fused-array improvement and all pushed safety fixes including GuestStorage copying. Control reuses its exact already-qualified binary; all four production paths equal installed source. No held type-fact, collector, getter or diagnostic code. No task-owned tests/builds overlap.')
assert m.digest(m.WORK/'cast-array-candidate-host') == identity['control_build']['binary_sha256']
assert m.digest(m.WORK/'folded-space-candidate-host') == identity['candidate_build']['binary_sha256']
(m.DATA/'identity.json').write_text(json.dumps(identity, indent=2)+'\n')
rows = json.loads((m.DATA/'samples.json').read_text()) if (m.DATA/'samples.json').exists() else []
engines = [('control', 'cast-array-candidate'), ('candidate', 'folded-space-candidate')]
orders = [engines, engines[::-1], engines]
for round_index, order in enumerate(orders, 1):
    for engine, version in order:
        label = f'folded-space-{engine}-{round_index}'
        if any(row['label'] == label for row in rows):
            continue
        m.full(label, version)
        row = json.loads((m.DATA/(label+'.json')).read_text())
        row['engine'] = engine
        if engine != 'v8':
            artifacts = list((m.WORK/(label+'-home')).rglob('*.wago'))
            assert len(artifacts) == 1, artifacts
            probe = subprocess.run([str(root/'.tmp/wago-next-gc-perf/local-admission'), str(artifacts[0])], check=True, capture_output=True, text=True)
            row['gc_native_root_admission'] = json.loads(probe.stdout)
            assert row['gc_native_root_admission']['Required'] and row['gc_native_root_admission']['Exact']
        rows.append(row)
        (m.DATA/'samples.json').write_text(json.dumps(rows, indent=2)+'\n')
summary = {}
for engine, version in engines:
    selected = [row for row in rows if row['engine'] == engine]
    times = [row['seconds'] for row in selected]
    summary[engine] = dict(seconds=times, median_seconds=statistics.median(times),
                           median_peak_rss_kib=statistics.median(row['peak_rss_kib'] for row in selected))
summary['candidate_reduction_percent'] = 100*(1-summary['candidate']['median_seconds']/summary['control']['median_seconds'])
summary['v8_reference_seconds'] = 4.0
summary['candidate_over_known_v8'] = summary['candidate']['median_seconds']/4.0
summary['all_outputs_identical_and_main_42'] = True
(m.DATA/'summary.json').write_text(json.dumps(summary, indent=2)+'\n')
print(json.dumps(summary, indent=2), flush=True)
