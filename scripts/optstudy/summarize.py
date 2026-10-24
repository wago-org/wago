#!/usr/bin/env python3
"""Summarize paired off/on ratios without hiding missing or failed samples."""
import collections
import json
import math
from pathlib import Path
import statistics
import sys


def main():
    root = Path(sys.argv[1])
    manifest = json.loads((root / 'manifest.json').read_text())
    rows = {}
    failures = []
    for path in root.glob('*.json'):
        if path.name == 'manifest.json':
            continue
        row = json.loads(path.read_text())
        if 'measurement' not in row:
            failures.append(row)
        else:
            rows[row['sample'], row['id'], row['option'], row['enabled']] = row['measurement']
    result = []
    metrics = ['compile_ns', 'allocated_bytes', 'compile_peak_rss_bytes', 'code_bytes', 'exec_ns']
    for opt in manifest['catalog']:
        by_id = collections.defaultdict(list)
        for (sample, ident, option, enabled), on in rows.items():
            if option != opt['name'] or not enabled:
                continue
            off = rows.get((sample, ident, option, False))
            if off is not None:
                by_id[ident].append((off, on))
        entry = {'name': opt['name'], 'default': opt['default'],
                 'paired_workloads': len(by_id), 'paired_samples': sum(map(len, by_id.values())),
                 'code_changed_workloads': sum(any(a['code_sha256'] != b['code_sha256'] for a, b in pairs)
                                               for pairs in by_id.values())}
        for metric in metrics:
            ratios = []
            for pairs in by_id.values():
                def value(row):
                    return statistics.median(row[metric]) if metric == 'exec_ns' else row[metric]
                ratios.append(statistics.median(value(a) / value(b) for a, b in pairs))
            # Equal workload weighting; ratio below 1 means disabling is cheaper.
            entry[metric + '_off_over_on'] = math.exp(statistics.mean(map(math.log, ratios))) if ratios else None
        result.append(entry)
    json.dump({'identity': {k: manifest[k] for k in ('source_sha', 'binary_sha256', 'quality')},
               'successful_samples': len(rows), 'failed_samples': len(failures),
               'failures': failures, 'options': result}, sys.stdout, indent=2)
    print()


if __name__ == '__main__':
    main()
