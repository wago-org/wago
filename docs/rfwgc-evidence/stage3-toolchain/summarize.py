#!/usr/bin/env python3
"""Summarize correctness outcomes only; no duration extraction or benchmarks."""
import json
import pathlib
import re
root = pathlib.Path(__file__).resolve().parent
blocked = re.findall(r'^--- FAIL: (\S+)', (root.parent / 'stage2-suite/ordinary.log').read_text(), re.M)
summary = {'previously_blocked': blocked, 'runs': {}}
for path in sorted(root.glob('*.log')):
    if not any(part in path.name for part in ('-staged-', '-harness-', '-converter-')):
        continue
    text = path.read_text()
    results = {name: state for state, name in re.findall(r'^--- (PASS|FAIL|SKIP): (\S+)', text, re.M)}
    summary['runs'][path.stem] = {
        'top_level': {state: sum(s == state for s in results.values()) for state in ('PASS', 'FAIL', 'SKIP')},
        'failures': [n for n, s in results.items() if s == 'FAIL'],
        'all_level_failures': re.findall(r'^\s*--- FAIL: (\S+)', text, re.M),
        'all_level_skips': re.findall(r'^\s*--- SKIP: (\S+)', text, re.M),
        'skips': [n for n, s in results.items() if s == 'SKIP'],
        'previously_blocked': {n: results.get(n, 'NOT RUN') for n in blocked} if '-staged-' in path.name else {},
    }
(root / 'summary.json').write_text(json.dumps(summary, indent=2) + '\n')
print(json.dumps({k:v['top_level'] for k,v in summary['runs'].items()}, indent=2))
