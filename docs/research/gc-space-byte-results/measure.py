#!/usr/bin/env python3
"""Run prebuilt control/candidate guest loops, with no concurrent builds."""
import hashlib
import json
import os
from pathlib import Path
import re
import statistics
import subprocess
import sys
import time

root = Path(__file__).resolve().parents[3]
data = Path(os.environ.get("WAGO_SPACE_BYTE_RESULTS", str(Path(__file__).resolve().parent)))
data.mkdir(parents=True, exist_ok=True)
control, candidate = map(Path, sys.argv[1:3])
affinity = sys.argv[3] if len(sys.argv) > 3 else "15"
env = dict(os.environ, GOMAXPROCS="1")
rows = json.loads((data / "micro-samples.json").read_text()) if (data / "micro-samples.json").exists() else []
pattern = re.compile(r"^BenchmarkGCResolverSpaceByte/([^\s]+?)(?:-\d+)?\s+(\d+)\s+([\d.]+) ns/op\s+(\d+) B/op\s+(\d+) allocs/op$", re.M)
identity = {
    "base": "049cb7e40bb70ce3555556a674066ec4151456bb",
    "affinity": affinity, "GOMAXPROCS": 1,
    "control_sha256": hashlib.sha256(control.read_bytes()).hexdigest(),
    "candidate_sha256": hashlib.sha256(candidate.read_bytes()).hexdigest(),
    "method": "ABBA; two samples per process; 200ms per guest loop; all samples retained",
}
identity_path = data / "micro-identity.json"
if rows and (not identity_path.exists() or json.loads(identity_path.read_text()) != identity):
    raise RuntimeError("retained samples belong to different binaries or settings")
identity_path.write_text(json.dumps(identity, indent=2) + "\n")
for index, (label, binary) in enumerate([("control", control), ("candidate", candidate), ("candidate", candidate), ("control", control)]):
    command = ["taskset", "-c", affinity, str(binary), "-test.run=^$", "-test.bench=^BenchmarkGCResolverSpaceByte$", "-test.benchmem", "-test.count=2", "-test.benchtime=200ms"]
    if index < len(rows):
        row = rows[index]
        if row["command"] != command or row["returncode"] != 0:
            raise RuntimeError("retained run does not match this command or failed")
        log = (data / f"micro-{index}-{label}.log").read_text()
    else:
        start = time.monotonic()
        result = subprocess.run(command, cwd=root / "src/wago", env=env, text=True, capture_output=True)
        seconds = time.monotonic() - start
        log = result.stdout + result.stderr
        (data / f"micro-{index}-{label}.log").write_text(log)
        row = {"label": label, "command": command, "seconds": seconds, "returncode": result.returncode}
        rows.append(row)
    samples = [{"case": m[0], "iterations": int(m[1]), "ns_op": float(m[2]), "bytes_op": int(m[3]), "allocs_op": int(m[4])} for m in pattern.findall(log)]
    row["samples"] = samples
    (data / "micro-samples.json").write_text(json.dumps(rows, indent=2) + "\n")
    print(json.dumps(row), flush=True)
    if row["returncode"] != 0 or len(samples) != 14:
        raise RuntimeError(f"invalid benchmark run: exit={row['returncode']}, samples={len(samples)}; see retained log")
summary = {}
for name in sorted({s["case"] for r in rows for s in r["samples"]}):
    values = {label: [s["ns_op"] for r in rows if r["label"] == label for s in r["samples"] if s["case"] == name] for label in ["control", "candidate"]}
    medians = {label: statistics.median(v) for label, v in values.items()}
    summary[name] = {"ns_op": values, "medians_ns_op": medians, "candidate_reduction_percent": 100 * (1 - medians["candidate"] / medians["control"])}
(data / "micro-summary.json").write_text(json.dumps(summary, indent=2) + "\n")
print(json.dumps(summary, indent=2), flush=True)
