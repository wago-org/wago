#!/usr/bin/env python3
"""Instrument table32 call_indirect indices in a Wasm module for issue #922.

Requires WABT wasm2wat/wat2wasm. Appends test-only globals and exports; no
production compiler/runtime code is changed. Only use when the observed table
has no guest mutation and the host runner does not mutate the exported table:
under that condition table index identifies a stable target during the run.
"""

import argparse
import json
import re
import subprocess
from pathlib import Path


FUNC = re.compile(r"^  \(func \(;([0-9]+);\)")
TOP_LEVEL = re.compile(r"^  \(")


def increment(name: str, indent: str) -> list[str]:
    return [
        f"{indent}global.get ${name}",
        f"{indent}i64.const 1",
        f"{indent}i64.add",
        f"{indent}global.set ${name}",
    ]


def probe(site: int, indent: str) -> list[str]:
    first = f"wago_s{site}_first"
    second = f"wago_s{site}_second"
    out = [f"{indent}local.tee $wago_indirect_index"]
    out += increment(f"wago_s{site}_total", indent)
    out += [
        f"{indent}global.get ${first}",
        f"{indent}i32.const -1",
        f"{indent}i32.eq",
        f"{indent}if",
        f"{indent}  local.get $wago_indirect_index",
        f"{indent}  global.set ${first}",
        f"{indent}end",
        f"{indent}local.get $wago_indirect_index",
        f"{indent}global.get ${first}",
        f"{indent}i32.eq",
        f"{indent}if",
    ]
    out += increment(f"wago_s{site}_first_hits", indent + "  ")
    out += [
        f"{indent}else",
        f"{indent}  global.get ${second}",
        f"{indent}  i32.const -1",
        f"{indent}  i32.eq",
        f"{indent}  if",
        f"{indent}    local.get $wago_indirect_index",
        f"{indent}    global.set ${second}",
        f"{indent}  end",
        f"{indent}  local.get $wago_indirect_index",
        f"{indent}  global.get ${second}",
        f"{indent}  i32.eq",
        f"{indent}  if",
    ]
    out += increment(f"wago_s{site}_second_hits", indent + "    ")
    out += [f"{indent}  else"]
    out += increment(f"wago_s{site}_other_hits", indent + "    ")
    out += [f"{indent}  end", f"{indent}end"]
    return out


def instrument_function(lines: list[str], sites: list[dict]) -> list[str]:
    match = FUNC.match(lines[0])
    if not match or not any(line.strip().startswith("call_indirect ") for line in lines):
        return lines
    function_index = int(match.group(1))
    first_instruction = 1
    while first_instruction < len(lines) and lines[first_instruction].strip().startswith("(local "):
        first_instruction += 1
    out = lines[:first_instruction] + ["    (local $wago_indirect_index i32)"]
    for line in lines[first_instruction:]:
        if line.strip().startswith("call_indirect "):
            site = len(sites)
            if "(table " in line and "(table 0)" not in line:
                raise ValueError(f"site {site}: only table 0 is supported")
            indent = line[: len(line) - len(line.lstrip())]
            out.extend(probe(site, indent))
            sites.append({"site": site, "function_index": function_index, "wat": line.strip()})
        out.append(line)
    return out


def transform(wat: str) -> tuple[str, list[dict]]:
    lines = wat.splitlines()
    out: list[str] = []
    block: list[str] = []
    sites: list[dict] = []
    for line in lines:
        if TOP_LEVEL.match(line) and block:
            out.extend(instrument_function(block, sites))
            block = []
        if TOP_LEVEL.match(line):
            block.append(line)
        elif block:
            block.append(line)
        else:
            out.append(line)
    if block:
        out.extend(instrument_function(block, sites))
    if not out or not out[-1].endswith(")"):
        raise ValueError("unexpected WABT module ending")
    # WABT normally places the module's final ')' on the last field's line.
    out[-1] = out[-1][:-1]
    additions: list[str] = []
    for site in sites:
        n = site["site"]
        for key in ("first", "second"):
            name = f"wago_s{n}_{key}"
            additions.append(f"  (global ${name} (mut i32) (i32.const -1))")
            additions.append(f'  (export "__{name}" (global ${name}))')
        for key in ("total", "first_hits", "second_hits", "other_hits"):
            name = f"wago_s{n}_{key}"
            additions.append(f"  (global ${name} (mut i64) (i64.const 0))")
            additions.append(f'  (export "__{name}" (global ${name}))')
    out.extend(additions)
    out.append(")")
    return "\n".join(out) + "\n", sites


def main() -> None:
    parser = argparse.ArgumentParser()
    parser.add_argument("source", type=Path)
    parser.add_argument("output", type=Path)
    args = parser.parse_args()
    wat = subprocess.check_output(["wasm2wat", "--enable-all", str(args.source)], text=True)
    instrumented, sites = transform(wat)
    if not sites:
        raise SystemExit("no call_indirect sites")
    wat_path = args.output.with_suffix(".wat")
    wat_path.write_text(instrumented)
    subprocess.run(["wat2wasm", "--enable-all", str(wat_path), "-o", str(args.output)], check=True)
    args.output.with_suffix(".sites.json").write_text(json.dumps(sites, indent=2) + "\n")


if __name__ == "__main__":
    main()
