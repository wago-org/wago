#!/usr/bin/env python3
"""Summarize a completed guarded capture, retaining run spread and VM noise."""
import argparse
import csv
import hashlib
import json
from pathlib import Path
from statistics import median


def describe(values):
    return {"median_ns": median(values), "min_ns": min(values),
            "max_ns": max(values), "samples": len(values)}


def summarize(directory):
    capture = json.loads((directory / 'capture.json').read_text())
    if capture['status'] != 'complete':
        raise ValueError('capture is not complete: ' + capture['status'])
    if any(s['blockers'] for s in capture['snapshots']):
        raise ValueError('capture contains interference')
    groups = {}
    for name, expected in capture['files'].items():
        path = directory / name
        if hashlib.sha256(path.read_bytes()).hexdigest() != expected:
            raise ValueError('CSV digest mismatch: ' + name)
        with path.open() as stream:
            for row in csv.reader(stream):
                key = (path.stem, row[1].split("-", 1)[0], int(row[2]))
                groups.setdefault(key, []).append(float(row[5]))
    result = {'capture': str(directory), 'blocks': capture.get('blocks', 4),
              'work': capture.get('work', 4194304), 'comparisons': [], 'reference': []}
    apis = ['instance', 'prepared', 'session']
    callbacks = sorted({key[1] for key in groups if key[0].startswith('baseline')})
    for api in apis:
        for callback in callbacks:
            for count in [1, 65536]:
                baseline, candidate, ratios = [], [], []
                for block in range(1, capture.get('blocks', 4) + 1):
                    before = groups[(f'baseline-{block}-{api}', callback, count)]
                    after = groups[(f'candidate-{block}-{api}', callback, count)]
                    baseline.extend(before)
                    candidate.extend(after)
                    ratios.append(median(after) / median(before))
                result['comparisons'].append({
                    'api': api, 'callback': callback, 'count': count,
                    'baseline': describe(baseline), 'candidate': describe(candidate),
                    'paired_candidate_over_baseline': ratios,
                    'median_paired_ratio': median(ratios)})
    for callback in sorted({key[1] for key in groups if key[0].startswith('reference')}):
        for count in [1, 65536]:
            before = groups[('reference-before', callback, count)]
            after = groups[('reference-after', callback, count)]
            result['reference'].append({
                'callback': callback, 'count': count, 'before': describe(before),
                'after': describe(after), 'combined': describe(before + after),
                'drift_percent': 100 * (median(after) / median(before) - 1)})
    snapshots = [json.loads(line) for line in
                 (directory / capture['snapshot_log']).read_text().splitlines()]
    affinity = snapshots[0].get('cpu_affinity', [])
    result['cpu_affinity'] = affinity
    result['vm_steal'] = {}
    for cpu in affinity:
        name = f'cpu{cpu}'
        ticks = [s['linux_cpu_ticks'][name] for s in snapshots
                 if name in s.get('linux_cpu_ticks', {})]
        if len(ticks) < 2:
            continue
        total = sum(ticks[-1][:8]) - sum(ticks[0][:8])
        stolen = ticks[-1][7] - ticks[0][7]
        intervals = []
        for before, after in zip(ticks, ticks[1:]):
            elapsed = sum(after[:8]) - sum(before[:8])
            if elapsed > 0:
                intervals.append(100 * (after[7] - before[7]) / elapsed)
        result['vm_steal'][name] = {
            'total_percent': 100 * stolen / total if total else None,
            'max_snapshot_interval_percent': max(intervals, default=0)}
    return result


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('capture', type=Path)
    args = parser.parse_args()
    try:
        result = summarize(args.capture)
    except (ValueError, KeyError) as error:
        parser.exit(1, str(error) + '\n')
    print(json.dumps(result, indent=2))


if __name__ == '__main__':
    main()
