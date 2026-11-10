#!/usr/bin/env python3
"""Reject a missing adapter cache key before any native call.

Use --runner /path/to/qemu-aarch64 on a non-ARM64 host. The overlay changes no
checkout files. Emulated correctness is not a native ARM64 performance result.
"""
from pathlib import Path
import argparse
import json
import os
import subprocess
import tempfile

ROOT = Path(__file__).resolve().parents[2]
PACKAGE = ROOT / "src/core/compiler/backend/railshot/arm64"
SOURCE = PACKAGE / "compile.go"
NEEDLE = "c.typ != ft || c.memSize != memSize || c.n == 0"
CANDIDATE = "c.candidate != ft || c.candidateMemSize != memSize"


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--runner", help="ARM64 user-mode emulator executable")
    args = parser.parse_args()
    original = SOURCE.read_text()
    if original.count(NEEDLE) != 1 or original.count(CANDIDATE) != 1:
        raise SystemExit("Expected both cache-key checks; inspect the control before use.")
    with tempfile.TemporaryDirectory(prefix="wago-arm64-reset-") as directory:
        work = Path(directory)
        replacement = work / "compile.go"
        replacement.write_text(original.replace(NEEDLE, "c.typ != ft || c.n == 0", 1)
                               .replace(CANDIDATE, "c.candidate != ft", 1))
        overlay = work / "overlay.json"
        overlay.write_text(json.dumps({"Replace": {str(SOURCE): str(replacement)}}))
        binary = work / "worker-reset.test"
        env = dict(os.environ, GOARCH="arm64")
        subprocess.run(
            ["go", "test", "-p", "1", "-c", "-tags=wago_codegenstats", "-overlay", str(overlay),
             "-o", str(binary), "./src/core/compiler/backend/railshot/arm64"],
            cwd=ROOT, env=env, check=True,
        )
        command = [str(binary), "-test.run=^TestWorkerScratchMatchesFresh$/^memory32$/^compact=false$"]
        if args.runner:
            command.insert(0, args.runner)
        result = subprocess.run(command, cwd=PACKAGE, text=True,
                                stdout=subprocess.PIPE, stderr=subprocess.STDOUT)
        print(result.stdout, end="")
        expected = "sequence=0 target=tiny mismatch: native code"
        if result.returncode != 1 or expected not in result.stdout:
            raise SystemExit("Control failed: the expected stale-adapter mismatch was not detected.")
        print("PASS: omitted cache key detected before the first native call.")


if __name__ == "__main__":
    main()
