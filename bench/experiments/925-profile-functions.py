#!/usr/bin/env python3
"""Instrument each local Wasm function's first entry with a visited bit.

The output is for disposable command profiling, never the production compiler.
Requires WABT wasm2wat/wat2wasm. Imports are excluded from the count.
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
    parser = argparse.ArgumentParser()
    parser.add_argument("source", type=Path)
    parser.add_argument("output", type=Path)
    args = parser.parse_args()
    with tempfile.TemporaryDirectory(prefix="wago-925-") as tmp:
        wat = Path(tmp) / "original.wat"
        rewritten = Path(tmp) / "instrumented.wat"
        subprocess.run(["wasm2wat", "--enable-all", str(args.source), "-o", str(wat)], check=True)
        functions = []
        with wat.open() as source, rewritten.open("w+") as output:
            pending = None
            inserted = True
            for line in source:
                if TOP.match(line):
                    if pending is not None and not inserted:
                        raise ValueError(f"function {pending} has no instruction insertion point")
                    match = FUNC.match(line)
                    pending = int(match.group(1)) if match else None
                    inserted = pending is None
                    if pending is not None:
                        functions.append(pending)
                        if line.count("(") == line.count(")"):
                            # WABT represents an empty function on one line.
                            line = line.rstrip("\n")[:-1] + (
                                f"\n    i32.const 1\n    global.set $wago_visited_{pending}\n  )\n"
                            )
                            inserted = True
                if pending is not None and not inserted and not FUNC.match(line) and not line.lstrip().startswith("(local "):
                    output.write(f"    i32.const 1\n    global.set $wago_visited_{pending}\n")
                    inserted = True
                output.write(line)
            output.seek(0, 2)
            size = output.tell()
            output.seek(size - 2)
            if output.read(2) != ")\n":
                raise ValueError("unexpected WABT module ending")
            output.truncate(size - 2)
            output.seek(0, 2)
            for index in functions:
                name = f"wago_visited_{index}"
                output.write(f"\n  (global ${name} (mut i32) (i32.const 0))")
                output.write(f'\n  (export "__{name}" (global ${name}))')
            output.write("\n)\n")
        subprocess.run(["wat2wasm", "--enable-all", str(rewritten), "-o", str(args.output)], check=True)
    args.output.with_suffix(".functions.json").write_text(json.dumps(functions) + "\n")


if __name__ == "__main__":
    main()
