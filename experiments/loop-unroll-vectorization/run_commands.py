#!/usr/bin/env python3
"""Serial, interleaved subprocess timing. Build both runners before this script."""
import json
import os
import pathlib
import subprocess
import sys
import time

root = pathlib.Path(__file__).resolve().parents[2]
os.chdir(root)
out = pathlib.Path(sys.argv[1] if len(sys.argv) > 1 else
                   "experiments/loop-unroll-vectorization/results/final/commands.jsonl")
if out.exists():
    raise SystemExit("Choose a new output file. Existing samples are preserved.")
env = {k: v for k, v in os.environ.items() if not k.startswith("WAGO_")}
env["GOMAXPROCS"] = "1"
base = "experiments/loop-unroll-vectorization/results/"
scalar = {k: "0" for k in ["WAGO_AMD64_VECTOR_MAP_LOOP", "WAGO_AMD64_ADJACENT_LOOP_PAIR",
          "WAGO_AMD64_WIDE_INDEPENDENT_LOOP", "WAGO_AMD64_WIDE_ADJACENT_LOOP",
          "WAGO_AMD64_SCALAR_MEMORY_RECURRENCE", "WAGO_AMD64_ZERO_COUNTER_LOOP"]}
with out.open("w") as f:
    for sample in range(6):
        jobs = []
        for repeat in [1, 1000]:
            for variant in ["baseline", "default", "A", "B", "H", "C", "D", "E", "G", "F"]:
                exe = base + ("workload-baseline" if variant == "baseline" else "workload")
                settings = {} if variant in ["baseline", "default"] else {"WAGO_LOOP_SUM_EXPERIMENT": variant}
                jobs.append((variant, exe, settings, ["-repeat", str(repeat), "-expected", "33558528"]))
        for workload, expected in [("jacobi-1d", 139987702), ("gemm", 122535361)]:
            for repeat in [1, 20]:
                for variant in ["baseline", "default", "scalar"]:
                    exe = base + ("workload-baseline" if variant == "baseline" else "workload")
                    jobs.append((variant, exe, scalar if variant == "scalar" else {},
                                 ["-wasm", f"corpus/workloads/polybench/{workload}.wasm", "-export",
                                  "polybench_run", "-repeat", str(repeat), "-expected", str(expected)]))
        if sample % 2:
            jobs.reverse()
        for variant, exe, settings, args in jobs:
            start = time.perf_counter_ns()
            result = subprocess.run([exe] + args, env=env | settings, capture_output=True,
                                    text=True, check=True)
            wall = time.perf_counter_ns() - start
            row = json.loads(result.stdout)
            row.update(variant=variant, sample=sample, wall_ns=wall, settings=settings,
                       command=[exe] + args)
            f.write(json.dumps(row, sort_keys=True) + "\n")
            f.flush()
