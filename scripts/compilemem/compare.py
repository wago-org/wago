#!/usr/bin/env python3
"""Run alternating fresh-process memory samples; never mix RSS and allocation."""
import argparse
import hashlib
import json
import os
from pathlib import Path
import subprocess

parser = argparse.ArgumentParser(description=__doc__)
parser.add_argument("--baseline", required=True, type=Path)
parser.add_argument("--candidate", required=True, type=Path)
parser.add_argument("--manifest", required=True, type=Path,
                    help="JSON array (or corpus array) of id/path/sha256 entries")
parser.add_argument("--root", default=Path.cwd(), type=Path)
parser.add_argument("--output", required=True, type=Path)
parser.add_argument("--repeat", default=5, type=int)
parser.add_argument("--cpu", default="0")
args = parser.parse_args()
if args.repeat < 1:
    parser.error("--repeat must be positive")
manifest = json.loads(args.manifest.read_text())
if isinstance(manifest, dict):
    manifest = manifest["corpus"]
for item in manifest:
    path = args.root / item["path"]
    digest = hashlib.sha256()
    with path.open("rb") as stream:
        for chunk in iter(lambda: stream.read(128 * 1024), b""):
            digest.update(chunk)
    actual = digest.hexdigest()
    if actual != item["sha256"]:
        raise SystemExit(f"input hash mismatch: {item['id']}")
binaries = {key: str(getattr(args, key).resolve())
            for key in ("baseline", "candidate")}
env = dict(os.environ, GOMAXPROCS="1")
with args.output.open("w") as output:
    for item in manifest:
        expected_code = None
        for repetition in range(args.repeat):
            order = ("baseline", "candidate")
            if repetition % 2:
                order = tuple(reversed(order))
            for revision in order:
                command = ["taskset", "-c", args.cpu, binaries[revision],
                           str(args.root / item["path"])]
                # A direct Python fork/exec carries Python's resident RSS
                # floor into the child high-water counter. The lightweight
                # shell must fork, not tail-exec, the harness. Positional
                # arguments preserve quoting and the harness exit status.
                launcher = ["/bin/sh", "-c",
                            '"$@"; rc=$?; exit "$rc"',
                            "compilemem-runner", *command]
                result = subprocess.run(launcher, env=env, check=True,
                                        capture_output=True, text=True)
                sample = json.loads(result.stdout)
                sample.update(id=item["id"], revision=revision,
                              repetition=repetition)
                output.write(json.dumps(sample) + "\n")
                output.flush()
                code = (sample["native_code_bytes"],
                        sample["native_code_sha256"])
                if expected_code is None:
                    expected_code = code
                elif code != expected_code:
                    raise SystemExit(f"native code mismatch: {item['id']}")
        print(f"{item['id']}: {args.repeat} pairs; native code identical",
              flush=True)
