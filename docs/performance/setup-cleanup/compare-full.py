#!/usr/bin/env python3
"""Compare the one-shot full runs; do not infer significance from one sample."""
from pathlib import Path
import json
import re

OUT = Path(__file__).resolve().parent


def read(label):
    text = (OUT / f'full-{label}.txt').read_text()
    if '\nPASS\n' not in text:
        raise SystemExit(f'{label}: capture has no PASS')
    rows = {}
    for line in text.splitlines():
        if not line.startswith('Benchmark'):
            continue
        fields = line.split()
        if len(fields) < 4 or not fields[1].isdigit():
            continue
        name = re.sub(r'-\d+$', '', fields[0])
        if name in rows:
            raise SystemExit(f'duplicate: {name}')
        rows[name] = {fields[i + 1]: float(fields[i]) for i in range(2, len(fields) - 1, 2)}
    return rows


baseline, candidate = read('baseline'), read('candidate')
if baseline.keys() != candidate.keys():
    raise SystemExit(f'case mismatch: {baseline.keys() ^ candidate.keys()}')
execution = [n for n in baseline if n.startswith('BenchmarkExec/')]
allocation_changes = []
timing_outliers = []
for name, old in baseline.items():
    new = candidate[name]
    for unit in ['B/op', 'allocs/op']:
        if old.get(unit) != new.get(unit):
            allocation_changes.append(dict(name=name, unit=unit, before=old.get(unit), after=new.get(unit)))
    ratio = new['ns/op'] / old['ns/op']
    if abs(ratio - 1) >= .10:
        timing_outliers.append(dict(name=name, before_ns=old['ns/op'], after_ns=new['ns/op'], percent=(ratio - 1) * 100))
result = dict(
    measurements_per_version=len(baseline),
    execution_cases=len(execution),
    execution_allocation_failures=[dict(version=label, name=n, metrics=rows[n]) for label, rows in [('baseline', baseline), ('candidate', candidate)] for n in execution if rows[n].get('B/op') != 0 or rows[n].get('allocs/op') != 0],
    allocation_changes=allocation_changes,
    timing_outliers_10_percent=sorted(timing_outliers, key=lambda row: row['percent'], reverse=True),
    caveat='One sample per full case; timing differences are flags, not statistical regression findings.',
)
(OUT / 'full-summary.json').write_text(json.dumps(result, indent=2) + '\n')
print(json.dumps({k: v for k, v in result.items() if k not in ['allocation_changes', 'timing_outliers_10_percent']}, indent=2))
print(f'Allocation metric changes: {len(allocation_changes)}; timing flags: {len(timing_outliers)}')
