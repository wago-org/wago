#!/usr/bin/env python3
"""Insert disposable counters before strict repeated scalar loads in WABT text.

This reproduces the source-level pattern only; it does not prove a native load.
"""
import argparse
import json
import re
import subprocess
import tempfile
from pathlib import Path

FUNC = re.compile(r"^  \(func \(;([0-9]+);\)")
TOP = re.compile(r"^  \(")
LOCAL = re.compile(r"^local\.get ([0-9]+)$")
LOAD = re.compile(r"^((?:i32|i64|f32|f64)\.load(?:8_[su]|16_[su]|32_[su])?)(?:\s|$)")
OFFSET = re.compile(r"\boffset=([0-9]+)")
BARRIER = re.compile(r"^(?:i(?:32|64)|f(?:32|64)|v128)\.store|^(?:call|return_call|memory\.|table\.|atomic\.|v128\.|local\.(?:set|tee)|block|loop|if|else|end|br|return|unreachable|try|catch|throw|delegate)")


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("source", type=Path)
    ap.add_argument("output", type=Path)
    args = ap.parse_args()
    with tempfile.TemporaryDirectory(prefix="wago-919-") as tmp:
        wat = Path(tmp) / "original.wat"
        rewritten = Path(tmp) / "instrumented.wat"
        subprocess.run(["wasm2wat", "--enable-all", str(args.source), "-o", str(wat)], check=True)
        sites = []
        function = None
        last_local = None
        prior = None
        with wat.open() as source, rewritten.open("w+") as output:
            for line in source:
                if TOP.match(line):
                    match = FUNC.match(line)
                    function = int(match.group(1)) if match else None
                    last_local = prior = None
                op = line.strip()
                local = LOCAL.match(op)
                load = LOAD.match(op)
                if function is not None and local:
                    last_local = int(local.group(1))
                elif function is not None and load:
                    offset = OFFSET.search(op)
                    key = (last_local, load.group(1), int(offset.group(1)) if offset else 0)
                    if last_local is not None:
                        if prior == key:
                            site = len(sites)
                            sites.append({"site": site, "function": function, "local": last_local, "opcode": key[1], "offset": key[2]})
                            indent = line[:len(line)-len(line.lstrip())]
                            output.write(f"{indent}global.get $wago_repeat_{site}\n{indent}i64.const 1\n{indent}i64.add\n{indent}global.set $wago_repeat_{site}\n")
                        prior = key
                    last_local = None
                else:
                    last_local = None
                    if BARRIER.match(op):
                        prior = None
                output.write(line)
            output.seek(0, 2)
            size = output.tell()
            output.seek(size-2)
            if output.read(2) != ")\n":
                raise ValueError("unexpected WABT module ending")
            output.truncate(size-2)
            output.seek(0, 2)
            for site in sites:
                name = f"wago_repeat_{site['site']}"
                output.write(f"\n  (global ${name} (mut i64) (i64.const 0))")
                output.write(f'\n  (export "__{name}" (global ${name}))')
            output.write("\n)\n")
        subprocess.run(["wat2wasm", "--enable-all", str(rewritten), "-o", str(args.output)], check=True)
    args.output.with_suffix(".sites.json").write_text(json.dumps(sites) + "\n")
    print(f"instrumented {len(sites)} strict repeated-load sites")


if __name__ == "__main__":
    main()
