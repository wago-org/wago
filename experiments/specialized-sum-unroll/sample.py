#!/usr/bin/env python3
"""Serial paired measurements. Build and test all binaries before running."""
import argparse
import json
import hashlib
import os
from pathlib import Path
import subprocess
import time

p = argparse.ArgumentParser()
p.add_argument("--baseline", required=True)
p.add_argument("--candidate", required=True)
p.add_argument("--baseline-variant", default="baseline")
p.add_argument("--variants", nargs="+", default=["A", "B"])
p.add_argument("--out", required=True)
p.add_argument("--samples", type=int, default=20)
p.add_argument("--cpu", type=int, default=min(os.sched_getaffinity(0)))
p.add_argument("--benchtime", default="150ms")
p.add_argument("--bench", default="^BenchmarkSumUnroll")
p.add_argument("--cwd", default=os.getcwd())
a = p.parse_args()
if a.samples < 20:
    p.error("decisive comparisons require at least 20 samples")
out = Path(a.out)
out.mkdir(parents=True, exist_ok=False)
env = dict(os.environ, GOMAXPROCS="1", GOFLAGS="-buildvcs=false")
metadata = dict(vars(a), started=time.strftime("%Y-%m-%dT%H:%M:%S%z"),
                affinity=sorted(os.sched_getaffinity(0)), environment={k:v for k,v in env.items() if k.startswith(("GO", "WAGO"))})
metadata["binary_sha256"] = {label: hashlib.sha256(Path(binary).read_bytes()).hexdigest() for label,binary in [("baseline",a.baseline),("candidate",a.candidate)]}
for cmd in (["go", "version"], ["lscpu"], ["uname", "-a"], ["git", "rev-parse", "HEAD"]):
    metadata[" ".join(cmd)] = subprocess.check_output(cmd, text=True).strip()
(out / "environment.json").write_text(json.dumps(metadata, indent=2)+"\n")
with (out / "order.jsonl").open("w") as order:
    for variant in a.variants:
        for sample in range(a.samples):
            pair = [("baseline", a.baseline, a.baseline_variant), ("candidate", a.candidate, variant)]
            if sample % 2:
                pair.reverse()
            for label, binary, selected in pair:
                local_env = dict(env, WAGO_SUM_VARIANT=selected)
                cmd = ["taskset", "-c", str(a.cpu), str(Path(binary).resolve()),
                       "-test.run", "^$", "-test.bench", a.bench,
                       "-test.benchtime", a.benchtime, "-test.count", "1"]
                started = time.time()
                r = subprocess.run(cmd, env=local_env, cwd=a.cwd, text=True,
                                   stdout=subprocess.PIPE, stderr=subprocess.STDOUT)
                with (out / f"{variant}-{label}.txt").open("a") as f:
                    f.write(r.stdout)
                order.write(json.dumps(dict(variant=variant, sample=sample, label=label,
                                            started=started, seconds=time.time()-started,
                                            returncode=r.returncode, cmd=cmd))+"\n")
                order.flush()
                if r.returncode or "PASS" not in r.stdout:
                    raise SystemExit(f"failed {variant}/{label}/{sample}: {r.stdout}")
            print(f"{variant}: {sample+1}/{a.samples}", flush=True)
        with (out / f"{variant}-benchstat.txt").open("w") as f:
            subprocess.run(["benchstat", str(out/f"{variant}-baseline.txt"),
                            str(out/f"{variant}-candidate.txt")], stdout=f, check=True)
