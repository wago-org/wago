"""Run the paired matrix from the repository root using prebuilt test binaries.

Run only while holding the exclusive Wago/Slipstream wall-time benchmark lease.
This script does not acquire the lease. Build and test before starting it.
"""

import os
from pathlib import Path
import subprocess

root = Path.cwd()
out = root / "docs/research/parallel-collection-results"
env = dict(os.environ, GOMAXPROCS="4")
matrices = [
    (
        "wasm",
        "/tmp/wago-parallel-wasm-before.test",
        "/tmp/wago-parallel-wasm-after.test",
        root / "src/wago",
        "^BenchmarkGCAllocationMixWorkers$",
        "200ms",
    ),
    (
        "full",
        "/tmp/wago-parallel-baseline-matrix.test",
        "/tmp/wago-parallel-after.test",
        root,
        "BenchmarkThroughputParallelGraph|BenchmarkThroughputFullLivePointerFree|"
        "BenchmarkThroughputFullOldHeapPause",
        "300ms",
    ),
    (
        "minor",
        "/tmp/wago-parallel-baseline-matrix.test",
        "/tmp/wago-parallel-after.test",
        root,
        "^BenchmarkThroughputParallelMinor$",
        "20x",
    ),
]
for name, before, after, cwd, pattern, benchtime in matrices:
    for stage in ["before", "after"]:
        (out / f"{name}-{stage}.txt").write_text("")
    for round_number in range(5):
        stages = [("before", before), ("after", after)]
        if round_number % 2:
            stages.reverse()
        for stage, binary in stages:
            with (out / f"{name}-{stage}.txt").open("a") as capture:
                subprocess.run(
                    [
                        "taskset", "-c", "8,10,12,14", binary, "-test.run=^$",
                        f"-test.bench={pattern}", f"-test.benchtime={benchtime}",
                        "-test.count=1",
                    ],
                    cwd=cwd,
                    env=env,
                    stdout=capture,
                    stderr=subprocess.STDOUT,
                    check=True,
                )
        print(f"{name}: pair {round_number + 1}/5 complete", flush=True)
