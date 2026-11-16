#!/usr/bin/env python3
"""Run only after explicit timing-window authorization; never builds binaries."""
import hashlib
import json
import os
from pathlib import Path
import subprocess
import sys


def digest(path):
    h = hashlib.sha256()
    with open(path, "rb") as f:
        for block in iter(lambda: f.read(1 << 20), b""):
            h.update(block)
    return h.hexdigest()


def main():
    baseline, candidate, output = map(Path, sys.argv[1:])
    paths = {"baseline": baseline.resolve(), "candidate": candidate.resolve()}
    result = {"binary_sha256": {k: digest(v) for k, v in paths.items()}, "runs": []}
    env = dict(os.environ, GOMAXPROCS="1")
    for round_ in range(3):
        for name in (("baseline", "candidate") if round_ % 2 == 0 else ("candidate", "baseline")):
            cmd = [str(paths[name]), "-test.run=^$", "-test.bench=^BenchmarkPayloadBoundary$", "-test.benchtime=100ms", "-test.count=1"]
            p = subprocess.run(cmd, env=env, text=True, capture_output=True, timeout=300)
            result["runs"].append({"round": round_, "variant": name, "command": cmd, "returncode": p.returncode, "stdout": p.stdout, "stderr": p.stderr})
            output.parent.mkdir(parents=True, exist_ok=True)
            output.write_text(json.dumps(result, indent=2) + "\n")
            if p.returncode:
                raise SystemExit(f"{name} failed; raw output saved at {output}")


if __name__ == "__main__":
    main()
