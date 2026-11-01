from pathlib import Path
import hashlib
import json
import re

here = Path(__file__).resolve().parent
results = {}
for path in sorted(here.glob('*.txt')):
    text = path.read_text()
    statuses = {s: re.findall(r'^\s*--- ' + s + r': (\S+)', text, re.M)
                for s in ('PASS', 'FAIL', 'SKIP')}
    if any(statuses.values()):
        results[path.name] = {'passes': len(statuses['PASS']),
                              'failures': statuses['FAIL'], 'skips': statuses['SKIP']}
for mode in ('ordinary', 'wagodebug', 'wago_regalloccheck'):
    for suffix in ('collector', 'guests', 'probes-artifacts'):
        assert not results[f'{mode}-{suffix}.txt']['failures']
    assert not results[f'{mode}-probes-artifacts.txt']['skips']
    def assertions(suffix):
        return [line.strip() for line in (here / f'{mode}-{suffix}.txt').read_text().splitlines()
                if 'interval_region_test.go:' in line and (':140:' in line or ':214:' in line)]
    assert len(assertions('backend')) == 2
    assert assertions('backend') == assertions('baseline-interval')
    assert set(results[f'{mode}-backend.txt']['failures']) == {
        'TestIntervalRegionDynamicReuse', 'TestIntervalRegionControlBoundaries',
        'TestIntervalRegionControlBoundaries/if-else'}
(here / 'results.json').write_text(json.dumps(results, indent=2) + '\n')
repo = here.parents[2]
probes = ['src/core/runtime/gc/native/native_review_stage4_test.go',
          'src/wago/native_review_stage4_amd64_test.go',
          'src/wago/native_review_stage4_reentry_amd64_test.go']
for file in probes:
    assert (repo / file).read_bytes() == (here / 'snapshot' / file).read_bytes()
paths = sorted(p for p in here.iterdir() if p.is_file() and p.name != 'SHA256SUMS'
               and p.suffix not in ('.tar', '.test'))
lines = [f'{hashlib.sha256(p.read_bytes()).hexdigest()}  {p.name}\n' for p in paths]
lines += [f'{hashlib.sha256((repo / p).read_bytes()).hexdigest()}  ../../../{p}\n' for p in probes]
(here / 'SHA256SUMS').write_text(''.join(lines))
