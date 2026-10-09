#!/usr/bin/env python3
"""Retain all numerical samples and produce a complete paired comparison."""
import argparse
import csv
import json
from pathlib import Path
import re
import statistics

def json_rows(value, depth=0):
    # Keep each benchmark sample array on one line for source review.
    if not isinstance(value, dict):
        return json.dumps(value, separators=(",", ":"))
    prefix = " " * (depth + 2)
    rows = [prefix + json.dumps(k) + ": " + json_rows(v, depth + 2) for k, v in value.items()]
    return "{\n" + ",\n".join(rows) + "\n" + " " * depth + "}"

p = argparse.ArgumentParser()
p.add_argument("directories", nargs="+")
a = p.parse_args()
pattern = re.compile(r"^(Benchmark\S+)\s+\d+\s+([\d.]+) ns/op[^\n]*?([\d.]+) B/op\s+([\d.]+) allocs/op", re.M)
for directory in a.directories:
    out = Path(directory)
    summary = {}
    rows = []
    for base in sorted(out.glob("*-baseline.txt")):
        variant = base.name.removesuffix("-baseline.txt")
        data = {}
        for label in ("baseline", "candidate"):
            data[label] = {}
            raw = (out/f"{variant}-{label}.txt").read_text()
            matches = pattern.findall(raw)
            if len(matches) != len(re.findall(r"^Benchmark",raw,re.M)):
                raise SystemExit(f"unparsed benchmark rows: {variant}/{label}")
            for name, ns, size, allocs in matches:
                data[label].setdefault(name, []).append([float(ns), float(size), float(allocs)])
        if data["baseline"].keys() != data["candidate"].keys():
            raise SystemExit(f"unmatched benchmark rows for {variant}")
        summary[variant] = {"samples":data,"paired_time_delta_percent":{name:[100*(c[0]/b[0]-1) for b,c in zip(samples,data["candidate"][name])] for name,samples in data["baseline"].items()}}
        for name, samples in data["baseline"].items():
            candidate = data["candidate"][name]
            if len(samples) < 20 or len(samples) != len(candidate):
                raise SystemExit(f"insufficient or unequal samples: {variant}/{name}")
            med = [statistics.median(x[i] for x in samples) for i in range(3)]
            cand = [statistics.median(x[i] for x in candidate) for i in range(3)]
            rows.append([variant, name, len(samples), *med, *cand, 100*(cand[0]/med[0]-1), statistics.median(100*(c[0]/b[0]-1) for b,c in zip(samples,candidate))])
    (out/"summary.json").write_text(json_rows(summary)+"\n")
    with (out/"comparison.csv").open("w") as f:
        writer = csv.writer(f, lineterminator="\n")
        writer.writerow(["variant", "benchmark", "samples_each", "baseline_ns", "baseline_bytes", "baseline_allocs",
                         "candidate_ns", "candidate_bytes", "candidate_allocs", "time_delta_percent", "median_paired_time_delta_percent"])
        writer.writerows(rows)
    print(f"{out}: {len(rows)} rows verified")
