#!/usr/bin/env python3
import csv
import re
import statistics
from pathlib import Path

root = Path(__file__).resolve().parent


def read(path):
    result = {}
    for line in path.read_text().splitlines():
        fields = line.split()
        if len(fields) < 4 or not fields[0].startswith("Benchmark") or fields[3] != "ns/op":
            continue
        name = re.sub(r"-\d+$", "", fields[0])
        metrics = {fields[i + 1]: float(fields[i]) for i in range(2, len(fields) - 1, 2)}
        result.setdefault(name, []).append(metrics)
    return result


rows = []
for profile in ["modern", "sse2"]:
    forms = {form: read(root / f"{form}-{profile}.txt") for form in ["split", "early", "late"]}
    assert forms["split"].keys() == forms["early"].keys() == forms["late"].keys()
    for name in forms["split"]:
        row = {"profile": profile, "benchmark": name, "samples_per_form": 7}
        for form, results in forms.items():
            samples = results[name]
            assert len(samples) == 7, (form, name, len(samples))
            for metric in ["ns/op", "ns/store", "code-B", "B/op", "allocs/op"]:
                row[f"{form}_{metric}"] = statistics.median(s[metric] for s in samples)
            assert row[f"{form}_B/op"] == row[f"{form}_allocs/op"] == 0
        row["late_vs_early_percent"] = (row["late_ns/op"] / row["early_ns/op"] - 1) * 100
        row["late_vs_split_percent"] = (row["late_ns/op"] / row["split_ns/op"] - 1) * 100
        if name.startswith("BenchmarkSignedImmediateStore/"):
            assert row["split_code-B"] == row["early_code-B"] == row["late_code-B"]
        rows.append(row)
        print(f"{profile} {name}: split={row['split_ns/op']:g} early={row['early_ns/op']:g} late={row['late_ns/op']:g} ns/op; late/early {row['late_vs_early_percent']:+.2f}%, late/split {row['late_vs_split_percent']:+.2f}%")

assert len(rows) == 24
with (root / "summary.csv").open("w", newline="") as output:
    writer = csv.DictWriter(output, fieldnames=list(rows[0]))
    writer.writeheader()
    writer.writerows(rows)
