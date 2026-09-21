#!/usr/bin/env python3
"""Validate one run before producing benchstat inputs; never read published samples implicitly."""
import argparse
import json
from pathlib import Path
import subprocess
import sys
sys.path.insert(0, str(Path(__file__).resolve().parents[1]))
from measurement import digest, fresh_directory, rows


def validate(directory):
    meta = json.loads((directory/'run.json').read_text())
    if meta['status'] != 'complete':
        raise ValueError('run is incomplete')
    expected = {(n, label, case) for n in range(1, meta['samples']+1) for label in ['baseline', 'candidate'] for case in meta['cases']}
    seen, leaves, outputs = set(), {}, {}
    for record in meta['records']:
        key = record['sample'], record['label'], record['case']
        if key not in expected or key in seen or record['run_id'] != meta['run_id'] or record['environment'] != meta['environment']:
            raise ValueError('duplicate sample or mixed configuration')
        seen.add(key)
        path = directory/record['output']
        if path.parent.resolve() != directory.resolve() or digest(path) != record['output_sha256']:
            raise ValueError('changed or invalid output file')
        parsed = rows(path.read_text())
        case = key[2]
        if sorted(parsed) != record['leaves'] or (case in leaves and leaves[case] != sorted(parsed)):
            raise ValueError('missing or mixed benchmark leaves')
        leaves[case] = sorted(parsed)
        outputs.setdefault((case, key[1]), []).append(path.read_text())
    if seen != expected:
        raise ValueError('missing paired samples')
    return outputs


def main():
    p = argparse.ArgumentParser(description=__doc__)
    p.add_argument('run', type=Path)
    p.add_argument('--output', required=True, type=Path)
    p.add_argument('--benchstat', default='benchstat')
    a = p.parse_args()
    outputs = validate(a.run)
    out = fresh_directory(a.output)
    for (case, label), texts in outputs.items():
        (out/f'{case}-{label}.txt').write_text('\n'.join(texts))
    for case in sorted({k[0] for k in outputs}):
        with (out/f'{case}-benchstat.txt').open('x') as f:
            subprocess.run([a.benchstat, str(out/f'{case}-baseline.txt'), str(out/f'{case}-candidate.txt')], stdout=f, check=True)


if __name__ == '__main__':
    main()
