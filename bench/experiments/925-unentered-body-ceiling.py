#!/usr/bin/env python3
"""Offline size/compile ceiling: replace unentered bodies with unreachable.

This is deliberately NOT a lazy compiler or semantically equivalent module for
other inputs. Call only after eager validation and same-input entry profiling.
"""
import argparse
import json
import re
import subprocess
import tempfile
from pathlib import Path

FUNC = re.compile(r"^  \(func \(;([0-9]+);\)")
TOP = re.compile(r"^  \(")


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("source", type=Path)
    ap.add_argument("entered_json", type=Path)
    ap.add_argument("output", type=Path)
    args = ap.parse_args()
    entered = set(json.loads(args.entered_json.read_text()))
    with tempfile.TemporaryDirectory(prefix="wago-925-ceiling-") as tmp:
        wat = Path(tmp) / "original.wat"
        changed = Path(tmp) / "ceiling.wat"
        subprocess.run(["wasm2wat", "--enable-all", str(args.source), "-o", str(wat)], check=True)
        omitted = 0
        suppress = False
        last_input = ""
        with wat.open() as source, changed.open("w+") as out:
            for line in source:
                last_input = line
                if TOP.match(line):
                    match = FUNC.match(line)
                    suppress = match is not None and int(match.group(1)) not in entered
                    if suppress:
                        omitted += 1
                        if line.count("(") == line.count(")"):
                            out.write(line.rstrip("\n")[:-1] + "\n    unreachable)\n")
                        else:
                            out.write(line)
                            out.write("    unreachable)\n")
                        continue
                if not suppress:
                    out.write(line)
            # WABT normally attaches the module's closing ')' to its last field.
            # If that field was suppressed, its closing ')' must be restored.
            if suppress:
                if not last_input.rstrip("\n").endswith("))"):
                    raise ValueError("unexpected suppressed final field")
                out.write(")\n")
        subprocess.run(["wat2wasm", "--enable-all", str(changed), "-o", str(args.output)], check=True)
    print(f"replaced {omitted} unentered local bodies (input-specific ceiling only)")


if __name__ == "__main__":
    main()
