"""Summarize completed alternating on/off pairs without dropping outliers."""
from collections import defaultdict
from pathlib import Path
from statistics import median, quantiles
import json
import sys

pairs = defaultdict(lambda: defaultdict(dict))
for filename in sys.argv[1:]:
    path = Path(filename)
    assert path.exists(), filename
    for line in path.read_text().splitlines():
        if not line.startswith('{'):
            continue
        row = json.loads(line)
        group = pairs[row['workload'], row['phase']][row['round']]
        assert row['on'] not in group, (filename, row)
        group[row['on']] = row['us']
assert pairs, 'no measurements'
print('| Workload | Phase | Pairs | Off median µs | On median µs | Median paired delta | Paired IQR | Pooled median delta |')
print('|---|---|---:|---:|---:|---:|---:|---:|')
for (name, phase), rounds in sorted(pairs.items()):
    assert all(len(pair) == 2 for pair in rounds.values()), (name, phase, rounds)
    off = [pair[False] for pair in rounds.values()]
    on = [pair[True] for pair in rounds.values()]
    ratios = [(pair[True] / pair[False] - 1) * 100 for pair in rounds.values()]
    quartiles = quantiles(ratios, n=4, method='inclusive')
    print(f'| {name} | {phase} | {len(rounds)} | {median(off):.4f} | {median(on):.4f} | {median(ratios):+.2f}% | {quartiles[0]:+.2f}% to {quartiles[2]:+.2f}% | {(median(on)/median(off)-1)*100:+.2f}% |')
print('\nNegative deltas mean faster. Paired ratios retain the alternating same-thread comparisons; pooled medians and interquartile ranges are reported too. Raw JSONL retains every observation. OS-thread locking and QoS do not establish physical-core affinity.')
