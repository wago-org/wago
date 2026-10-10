from pathlib import Path
from collections import defaultdict
from statistics import median
import re
import hashlib
import json

root = Path(__file__).resolve().parent
samples = defaultdict(list)
for line in (root / 'remaining-retained-refresh.txt').read_text().splitlines():
    match = re.match(r'BenchmarkParity/([^/]+)/Exec-\d+\s+\d+\s+([\d.]+) ns/op', line)
    if match:
        samples[match[1]].append(float(match[2]) / 1000)
assert len(samples) == 10 and all(len(v) == 2 for v in samples.values()), dict(samples)
assert (root / 'remaining-retained-refresh.txt').read_text().rstrip().endswith('PASS')
recorded = {}
for line in (root.parent.parent / 'steady-exec-wago-vs-w2c2.md').read_text().splitlines():
    match = re.match(r'\| \d+ \| `applications/([^`]+)` \| [\d.]+ \| ([\d.]+) \|', line)
    if match:
        recorded[match[1]] = float(match[2])
assert all(name in recorded for name in samples)
confirmed = set()
confirmation = root / 'remaining-retained-confirm.jsonl'
if confirmation.exists():
    replacement_samples = defaultdict(list)
    for line in confirmation.read_text().splitlines():
        if line.startswith('{'):
            row = json.loads(line)
            if row['on']:
                replacement_samples[row['workload']].append(row['us'])
    assert len(replacement_samples) == 3 and all(len(v) == 8 for v in replacement_samples.values())
    for name, values in replacement_samples.items():
        assert name in samples
        samples[name] = values
        confirmed.add(name)
rows = sorted(((name, median(values), recorded[name]) for name, values in samples.items()), key=lambda row: row[1] / row[2], reverse=True)
manifest = Path('/Users/work/Code/Web/wasm.fyi/corpora/applications/manifest.json')
text = '''# Remaining retained arm64 gaps: focused refresh

Apple M4 Max, arm64. Frozen fully qualified retained branch-vector build, with the early-select experiment excluded. Two100ms execution samples per workload, with three anomalous/variable cases replaced by eight300ms same-thread/QoS confirmation samples from the enabled retained branch-vector build. Exact checks supplied by both harnesses. The w2c2 column is the historical device capture from the complete ranking, not a simultaneous paired measurement. This ten-case subset is for prioritization; it does not establish full-corpus parity or replace the original recorded ranking. Times are microseconds per invocation.

| Corpus | Retained Wago median | Sample range | Recorded w2c2 | Ratio | Samples |
|---|---:|---:|---:|---:|---|
'''
for name, value, baseline in rows:
    values = samples[name]
    text += f'| {name} | {value:.3f} | {min(values):.3f}–{max(values):.3f} | {baseline:.3f} | {value/baseline:.3f}× | {"8 paired enabled" if name in confirmed else "2 harness"} |\n'
text += '\nEvidence: `remaining-retained-refresh.txt`; binary `/tmp/parity-branch-vector-retained.test`. Current manifest SHA256: `' + hashlib.sha256(manifest.read_bytes()).hexdigest() + '`. The binary was qualified on all148contracts in both bounds modes before this refresh. No new full-set timing sweep.\n'
(root / 'remaining-retained-current-subset.md').write_text(text)
print(text)

if confirmed:
    path = root / 'remaining-retained-current-subset.md'
    path.write_text(path.read_text() + '\nRaw broad-harness KNN99.201µs and VM22.438µs anomalies are preserved in the raw log but replaced here by stable same-thread confirmations. ADPCM is also confirmed because its two original samples varied. Protocols differ, so nearby priority ordering remains provisional. Neither physical-core affinity nor simultaneous w2c2 measurement is claimed.\n')
