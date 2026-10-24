"""Build a serial collector overlay with the candidate benchmark fixtures.

Run from the repository root. The output path is used by the commands in
../parallel-collection.md; production collector code comes from the base commit.
"""

import json
from pathlib import Path
import subprocess
import sys

root = Path.cwd()
base = sys.argv[1] if len(sys.argv) > 1 else "fc6af651c"
subprocess.run(
    ["git", "rev-parse", "--verify", base],
    check=True,
    stdout=subprocess.DEVNULL,
)
out = Path("/tmp/wago-parallel-baseline-overlay")
out.mkdir(exist_ok=True)
replace = {}
files = subprocess.check_output(
    ["git", "diff", base, "--name-only", "--", "src/core/runtime/gc/native"],
    text=True,
).splitlines()
for i, name in enumerate(files):
    exists = subprocess.run(
        ["git", "cat-file", "-e", f"{base}:{name}"], stderr=subprocess.DEVNULL
    )
    if exists.returncode:
        continue
    original = out / f"original-{i}.go"
    original.write_bytes(subprocess.check_output(["git", "show", f"{base}:{name}"]))
    replace[str(root / name)] = str(original)

empty = out / "empty.go"
empty.write_text("package gc\n")
for candidate in (root / "src/core/runtime/gc/native").glob("parallel*.go"):
    if candidate.name == "parallel_bench_test.go":
        target = out / "parallel_bench_test.go"
        target.write_text(candidate.read_text().replace("Workers: workers,", ""))
        replace[str(candidate)] = str(target)
    else:
        replace[str(candidate)] = str(empty)

# The baseline lacks worker configuration: both labels execute serial code.
for candidate in (root / "src/wago").glob("gc_allocation_mix*test.go"):
    target = out / "allocation_mix_test.go"
    target.write_text(candidate.read_text().replace("Workers: workers,", ""))
    replace[str(candidate)] = str(target)
empty_wago = out / "emptywago.go"
empty_wago.write_text("package wago\n")
replace[str(root / "src/wago/gc_parallel_test.go")] = str(empty_wago)
(out / "overlay.json").write_text(json.dumps({"Replace": replace}, indent=2))
