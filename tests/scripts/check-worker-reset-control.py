#!/usr/bin/env python3
"""Check that the fresh-worker test detects an omitted CPU-feature reset.

Run from any directory. Requires Go. The temporary Go overlay changes no
checkout file. The selected test must reject stale state before a native call.
"""
from pathlib import Path
import json
import subprocess
import tempfile

ROOT = Path(__file__).resolve().parents[2]
PACKAGE = ROOT / "src/core/compiler/backend/railshot/amd64"
SOURCE = PACKAGE / "compile.go"
NEEDLE = "\tsc.usedAMD64Features = 0\n"


def main():
    original = SOURCE.read_text()
    if original.count(NEEDLE) != 1:
        raise SystemExit("Expected one CPU-feature reset; inspect the control before use.")
    with tempfile.TemporaryDirectory(prefix="wago-worker-reset-") as directory:
        work = Path(directory)
        replacement = work / "compile.go"
        replacement.write_text(original.replace(NEEDLE, "\t// Omitted reset: test control only.\n", 1))
        overlay = work / "overlay.json"
        overlay.write_text(json.dumps({"Replace": {str(SOURCE): str(replacement)}}))
        binary = work / "worker-reset.test"
        subprocess.run(
            ["go", "test", "-c", "-tags=wago_codegenstats", "-overlay", str(overlay),
             "-o", str(binary), "./src/core/compiler/backend/railshot/amd64"],
            cwd=ROOT, check=True,
        )
        result = subprocess.run(
            [str(binary), "-test.run=^TestWorkerScratchMatchesFresh$/^memory32$/^compact=false$/^features=f$"],
            cwd=PACKAGE, text=True, stdout=subprocess.PIPE, stderr=subprocess.STDOUT,
        )
        print(result.stdout, end="")
        expected = "sequence=0 target=tiny mismatch: CPU feature metadata; features got=a want=0"
        if result.returncode != 1 or expected not in result.stdout:
            raise SystemExit("Control failed: the expected stale-feature mismatch was not detected.")
        print("PASS: omitted reset detected before the first native call.")


if __name__ == "__main__":
    main()
