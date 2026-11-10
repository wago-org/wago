import argparse
import csv
import json
import os
from pathlib import Path
import re
import statistics
import subprocess


parser = argparse.ArgumentParser(
    description="Replay the archived integer join-pressure comparison."
)
parser.add_argument(
    "evidence",
    type=Path,
    help="directory containing main-amd64.test and pr-amd64.test",
)
args = parser.parse_args()
root = args.evidence.resolve()

if not root.is_dir():
    parser.error(f"evidence directory does not exist: {root}")
for revision in ("main", "pr"):
    binary = root / f"{revision}-amd64.test"
    if not binary.is_file():
        parser.error(f"test binary does not exist: {binary}")

env = {key: value for key, value in os.environ.items() if not key.startswith("WAGO_")}
env.update(GOMAXPROCS="1", WAGO_BOUNDS="explicit")
records = []
for pair in range(1, 9):
    order = ["main", "pr"] if pair % 2 else ["pr", "main"]
    for ordinal, revision in enumerate(order, 1):
        cmd = [
            "taskset",
            "-c",
            "4",
            str(root / f"{revision}-amd64.test"),
            "-test.run=^$",
            "-test.bench=^BenchmarkIntegerLiveAcrossJoin$",
            "-test.benchtime=100ms",
            "-test.count=1",
            "-test.benchmem",
            "-test.cpu=1",
        ]
        result = subprocess.run(cmd, env=env, capture_output=True, text=True)
        stem = f"pair-{pair:02}-{ordinal}-{revision}"
        (root / f"{stem}.stdout").write_text(result.stdout)
        (root / f"{stem}.stderr").write_text(result.stderr)
        (root / f"{stem}.exit").write_text(f"{result.returncode}\n")
        if result.returncode:
            raise SystemExit(result.returncode)
        rows = 0
        for line in result.stdout.splitlines():
            match = re.match(
                r"^BenchmarkIntegerLiveAcrossJoin/(\S+)\s+\d+\s+([\d.]+) ns/op\s+"
                r"([\d.]+) native-B\s+(\d+) B/op\s+(\d+) allocs/op",
                line,
            )
            if not match:
                continue
            rows += 1
            records.append(
                {
                    "pair": pair,
                    "order": ordinal,
                    "revision": revision,
                    "benchmark": match[1],
                    "ns_per_op": float(match[2]),
                    "native_bytes": int(float(match[3])),
                    "bytes_per_op": int(match[4]),
                    "allocs_per_op": int(match[5]),
                }
            )
        if rows != 12:
            raise RuntimeError(f"{stem}: {rows} rows")
    print(f"pair {pair}/8 complete", flush=True)

with (root / "integer-join-pressure-20261019.csv").open("w") as output:
    writer = csv.DictWriter(output, fieldnames=records[0].keys())
    writer.writeheader()
    writer.writerows(records)

summary = []
for name in dict.fromkeys(record["benchmark"] for record in records):
    rows = [record for record in records if record["benchmark"] == name]
    main = [record["ns_per_op"] for record in rows if record["revision"] == "main"]
    pr = [record["ns_per_op"] for record in rows if record["revision"] == "pr"]
    delta = [(candidate / baseline - 1) * 100 for candidate, baseline in zip(pr, main)]
    resource = {
        key: sorted(set(record[key] for record in rows))
        for key in ["native_bytes", "bytes_per_op", "allocs_per_op"]
    }
    summary.append(
        {
            "benchmark": name,
            "main_median": statistics.median(main),
            "pr_median": statistics.median(pr),
            "delta": statistics.median(delta),
            "delta_min": min(delta),
            "delta_max": max(delta),
            "main_cv": 100 * statistics.stdev(main) / statistics.mean(main),
            "pr_cv": 100 * statistics.stdev(pr) / statistics.mean(pr),
            **resource,
        }
    )
(root / "summary.json").write_text(json.dumps(summary, indent=2) + "\n")
for row in summary:
    print(
        f"| `{row['benchmark']}` | {row['main_median']:.2f} | "
        f"{row['pr_median']:.2f} | {row['delta']:+.2f}% "
        f"[{row['delta_min']:+.2f}%, {row['delta_max']:+.2f}%] | "
        f"{row['main_cv']:.2f}% / {row['pr_cv']:.2f}% |"
    )
