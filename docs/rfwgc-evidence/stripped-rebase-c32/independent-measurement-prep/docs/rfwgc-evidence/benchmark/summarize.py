#!/usr/bin/env python3
"""Summarize retained Go benchmark samples; time effects use ratios of medians."""
import pathlib, re, statistics, sys
raw = pathlib.Path(sys.argv[1])
rows = {}
for kind in ['qualifying', 'nonqualifying', 'no-gc']:
    for variant in ['baseline-off', 'port-off', 'port-on']:
        samples = []
        for path in sorted(raw.glob(f'??-{kind}-{variant}.txt')):
            match = re.search(r'^BenchmarkGCLifetimeCompile/\S+\s+\d+\s+([\d.]+) ns/op\s+(\d+) B/op\s+(\d+) allocs/op', path.read_text(), re.M)
            if not match: raise ValueError(path)
            samples.append(tuple(map(float, match.groups())))
        if len(samples) < 6: raise ValueError(f'Only {len(samples)} samples: {kind}/{variant}')
        med = [statistics.median(s[i] for s in samples) for i in range(3)]
        times = [s[0] for s in samples]
        rows[kind, variant] = med
        base = rows[kind,'baseline-off'][0]; off = rows.get((kind,'port-off'),med)[0]
        print(f'| {kind} | {variant} | {len(samples)} | {med[0]/1000:.2f} | {med[1]:.0f} | {med[2]:.0f} | {(med[0]/base-1)*100:+.2f}% | {(med[0]/off-1)*100:+.2f}% | {min(times)/1000:.2f}–{max(times)/1000:.2f} | {statistics.stdev(times)/statistics.mean(times)*100:.2f}% |')
