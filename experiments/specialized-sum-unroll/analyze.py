#!/usr/bin/env python3
"""Retain all numerical samples and produce a complete paired comparison."""
import argparse
import csv
import json
from pathlib import Path
import re
import statistics

p = argparse.ArgumentParser()
p.add_argument("directories", nargs="+")
a = p.parse_args()
pattern = re.compile(r"^(Benchmark\S+)\s+\d+\s+([\d.]+) ns/op\s+([\d.]+) B/op\s+([\d.]+) allocs/op", re.M)
for directory in a.directories:
    out = Path(directory)
    summary = {}
    rows = []
    for base in sorted(out.glob("*-baseline.txt")):
        variant = base.name.removesuffix("-baseline.txt")
        data = {}
        for label in ("baseline", "candidate"):
            data[label] = {}
            for name, ns, size, allocs in pattern.findall((out/f"{variant}-{label}.txt").read_text()):
                data[label].setdefault(name, []).append([float(ns), float(size), float(allocs)])
        if data["baseline"].keys() != data["candidate"].keys():
            raise SystemExit(f"unmatched benchmark rows for {variant}")
        summary[variant] = data
        for name, samples in data["baseline"].items():
            candidate = data["candidate"][name]
            if len(samples) < 20 or len(samples) != len(candidate):
                raise SystemExit(f"insufficient or unequal samples: {variant}/{name}")
            med = [statistics.median(x[i] for x in samples) for i in range(3)]
            cand = [statistics.median(x[i] for x in candidate) for i in range(3)]
            rows.append([variant, name, len(samples), *med, *cand, 100*(cand[0]/med[0]-1)])
    (out/"summary.json").write_text(json.dumps(summary, indent=2)+"\n")
    with (out/"comparison.csv").open("w") as f:
        writer = csv.writer(f)
        writer.writerow(["variant", "benchmark", "samples_each", "baseline_ns", "baseline_bytes", "baseline_allocs",
                         "candidate_ns", "candidate_bytes", "candidate_allocs", "time_delta_percent"])
        writer.writerows(rows)
    print(f"{out}: {len(rows)} rows verified")
