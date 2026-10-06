#!/usr/bin/env python3
import csv
import math
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
    before, after = [read(root / f"{form}-{profile}.txt") for form in ["before", "after"]]
    assert before.keys() == after.keys()
    for name in before:
        samples = [before[name], after[name]]
        assert all(len(s) == 7 for s in samples), name
        row = {"profile": profile, "benchmark": name, "samples_per_form": 7}
        for metric in ["ns/op", "ns/store", "B/op", "allocs/op", "code-B"]:
            for form, sample in zip(["before", "after"], samples):
                row[f"{form}_{metric}"] = statistics.median(x[metric] for x in sample)
        row["change_percent"] = (row["after_ns/op"] / row["before_ns/op"] - 1) * 100
        assert row["before_B/op"] == row["after_B/op"] == 0
        assert row["before_allocs/op"] == row["after_allocs/op"] == 0
        if name.startswith("BenchmarkSignedImmediateStore/"):
            assert row["before_code-B"] == row["after_code-B"]
        rows.append(row)
    for mode, prefix in [("explicit", "BenchmarkSignedImmediateStore/"), ("guard", "BenchmarkGuardSignedImmediateStore/")]:
        group = [r for r in rows if r["profile"] == profile and r["benchmark"].startswith(prefix)]
        geomean = math.exp(statistics.mean(math.log(r["after_ns/op"] / r["before_ns/op"]) for r in group))
        print(f"{profile} {mode}: {len(group)} rows; geomean {(geomean - 1) * 100:+.2f}%")
        for r in group:
            print(f"  {r['benchmark']}: {r['before_ns/op']:g} -> {r['after_ns/op']:g} ns/op ({r['change_percent']:+.2f}%); code {r['before_code-B']:g} -> {r['after_code-B']:g} B")

assert len(rows) == 60
with (root / "summary.csv").open("w", newline="") as output:
    writer = csv.DictWriter(output, fieldnames=list(rows[0]))
    writer.writeheader()
    writer.writerows(rows)
