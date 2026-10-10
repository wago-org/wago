"""Summarize each candidate against its paired baseline, preserving round ratios."""
import pathlib
import re
import statistics

OUT = pathlib.Path(__file__).resolve().parent
PATTERN = re.compile(r"BenchmarkParity/([^/]+)/(Compile|Exec)-\d+\s+\d+\s+([\d.]+) ns/op")


def samples(path):
    values = {}
    for case, phase, ns in PATTERN.findall(path.read_text()):
        values.setdefault((case, phase), []).append(float(ns))
    return {key: statistics.median(items) for key, items in values.items()}


rows = {}
for path in sorted(OUT.glob("policy-probe-*-*.txt")):
    if not path.read_text().rstrip().endswith("PASS"):
        continue
    suffix = path.stem.removeprefix("policy-probe-")
    round_number, label = suffix.split("-", 1)
    if "base-before-" in label:
        continue
    base_label = "layout-base-before-" + label.removeprefix("layout-") if label.startswith("layout-") else "base-before-" + label
    base_path = OUT / f"policy-probe-{round_number}-{base_label}.txt"
    if not base_path.exists() or not base_path.read_text().rstrip().endswith("PASS"):
        continue
    baseline = samples(base_path)
    for key, value in samples(path).items():
        if key in baseline:
            rows.setdefault((label, *key), []).append(100 * (value / baseline[key] - 1))

print("variant\tcase\tphase\tpaired change % per round")
for key, ratios in sorted(rows.items()):
    print("\t".join(key) + "\t" + ", ".join(f"{ratio:+.1f}" for ratio in ratios))
