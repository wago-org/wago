#!/usr/bin/env python3
"""Retain benchmark samples and emit medians. Benchstat supplies significance."""
import json
import pathlib
import re
import statistics
import sys

root = pathlib.Path(sys.argv[1] if len(sys.argv) > 1 else
                    "experiments/loop-unroll-vectorization/results/final")
summary = {}
for path in sorted(root.glob("*.txt")):
    rows = {}
    for line in path.read_text().splitlines():
        if not line.startswith("Benchmark"):
            continue
        fields = line.split()
        if "ns/op" not in fields:
            continue
        name = re.sub(r"-\d+$", "", fields[0])
        row = {unit: float(fields[fields.index(unit) - 1])
               for unit in ["ns/op", "B/op", "allocs/op"]}
        rows.setdefault(name, []).append(row)
    if not rows:
        continue
    summary[path.name] = {}
    for name, samples in rows.items():
        if len(samples) != 6:
            raise SystemExit(f"{path.name}: {name}: {len(samples)} samples; expected 6")
        summary[path.name][name] = {
            "n": len(samples), "samples": samples,
            "median": {unit: statistics.median(s[unit] for s in samples)
                       for unit in samples[0]}}
(root / "summary.json").write_text(json.dumps(summary, indent=2) + "\n")
commands = root / "commands.jsonl"
if commands.exists():
    groups = {}
    for line in commands.read_text().splitlines():
        row = json.loads(line)
        key = f"{pathlib.Path(row['wasm']).name}/{row['repeat']}/{row['variant']}"
        groups.setdefault(key, []).append(row)
    output = {}
    for key, rows in groups.items():
        if len(rows) != 6:
            raise SystemExit(f"{key}: {len(rows)} subprocess samples; expected 6")
        output[key] = {k: statistics.median(r[k] for r in rows)
                       for k in ["wall_ns", "compile_ns", "instantiate_ns", "execute_ns",
                                 "compile_bytes", "compile_allocs", "remaining_bytes", "remaining_allocs"]}
    (root / "commands-summary.json").write_text(json.dumps(output, indent=2) + "\n")
print(f"Verified six samples for {sum(len(v) for v in summary.values())} benchmark rows.")
