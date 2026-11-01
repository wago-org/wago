#!/usr/bin/env python3
"""Run only bounded probes in pre-extracted immutable source archives.
No benchmarks, source crash fixtures, cross-compilation, or production edits.
"""
import hashlib
import json
import pathlib
import subprocess
import sys

root = pathlib.Path(__file__).resolve().parent
go = sys.argv[1] if len(sys.argv) > 1 else "/home/dev/.local/share/mise/installs/go/1.27.3/bin/go"
pin = sys.argv[2]
probe = root / "testdata/r9_ownership_state_lifecycle_test.go"
archive = root / "snapshots" / pin
assert (archive / "src/wago/r9_ownership_state_lifecycle_test.go").read_bytes() == probe.read_bytes()
results = []
for mode in ("ordinary", "wagodebug", "wago_regalloccheck"):
    cmd = [go, "test", "./src/wago", "-run", "^TestR9OwnershipState", "-count=1", "-v"]
    if mode != "ordinary":
        cmd += ["-tags", mode]
    with (root / f"{pin}-{mode}.log").open("w") as out:
        result = subprocess.run(cmd, cwd=archive, stdout=out, stderr=subprocess.STDOUT, timeout=600)
    results.append(dict(pin=pin, mode=mode, command=cmd, exit_code=result.returncode,
                        probe_sha256=hashlib.sha256(probe.read_bytes()).hexdigest()))
    print(pin, mode, result.returncode, flush=True)
(root / f"{pin}-results.json").write_text(json.dumps(results, indent=2) + "\n")
