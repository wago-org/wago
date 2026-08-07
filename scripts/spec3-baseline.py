#!/usr/bin/env python3
"""Convert a full make spec3 log into the committed Release 3 red inventory."""

import argparse
import json
import re
from pathlib import Path

FILE_RE = re.compile(
    r"spectest_exec_test\.go:\d+: (?P<file>\S+)\s+"
    r"modules\(pass=(?P<mp>\d+) fail=(?P<mf>\d+) skip=(?P<ms>\d+)\) "
    r"assertions\(pass=(?P<ap>\d+) fail=(?P<af>\d+) skip=(?P<as>\d+)\) "
    r"gaps\((?P<gaps>.*)\)$"
)
PARSER_RE = re.compile(
    r"spectest_exec_test\.go:\d+: (?P<file>\S+): "
    r"(?P<error>(?:wast2json failed|Release 3 .*conversion failed|Release 3 converter unavailable).*)$"
)
FALLBACK_RE = re.compile(
    r"spectest_exec_test\.go:\d+: (?P<file>\S+): "
    r"text oracle fallback=WebAssembly/spec interpreter"
)
TOTAL_RE = re.compile(
    r"TOTAL\[3\.0\]: modules passed=(?P<mp>\d+) failed=(?P<mf>\d+) skipped=(?P<ms>\d+) \| "
    r"assertions passed=(?P<ap>\d+) failed=(?P<af>\d+) skipped=(?P<as>\d+) \| gaps (?P<gaps>.*)$"
)


def counts(match):
    return {
        "modules": {"passed": int(match["mp"]), "failed": int(match["mf"]), "skipped": int(match["ms"])},
        "assertions": {"passed": int(match["ap"]), "failed": int(match["af"]), "skipped": int(match["as"])},
        "gaps": {k: int(v) for k, v in (item.split("=", 1) for item in match["gaps"].split())},
    }


def family(name):
    if name == "const":
        return "extended-constant-expressions"
    if name in {"return_call", "return_call_indirect", "return_call_ref"}:
        return "tail-calls"
    if name.startswith("exceptions/"):
        return "exception-handling"
    if name.startswith("multi-memory/") or name == "simd/simd_memory-multi":
        return "multi-memory"
    if name.startswith("memory64/"):
        return "table64" if Path(name).name.startswith("table64") else "memory64"
    if name.startswith("relaxed-simd/"):
        return "relaxed-simd"
    if name.startswith("gc/") or name in {"type-canon", "type-equivalence", "type-rec"}:
        return "gc"
    typed_roots = {
        "br_on_non_null", "br_on_null", "call_ref", "ref", "ref_as_non_null",
        "ref_is_null", "ref_null", "return_call_ref", "unreached-valid",
    }
    if name in typed_roots:
        return "typed-function-references"
    return "core-and-cross-cutting"


def add_counts(dst, src):
    for section in ("modules", "assertions", "gaps"):
        for key, value in src[section].items():
            dst[section][key] = dst[section].get(key, 0) + value


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("log", type=Path)
    parser.add_argument("output", type=Path)
    parser.add_argument("--exit-code", type=int, required=True)
    args = parser.parse_args()

    entries = {}
    fallbacks = set()
    total = None
    for raw in args.log.read_text(errors="replace").splitlines():
        if match := PARSER_RE.search(raw):
            entries[match["file"]] = {
                "file": match["file"], "family": family(match["file"]),
                "status": "parser-failure", "parser_error": match["error"],
            }
            continue
        if match := FALLBACK_RE.search(raw):
            fallbacks.add(match["file"])
            continue
        if match := FILE_RE.search(raw):
            c = counts(match)
            red = c["modules"]["failed"] + c["modules"]["skipped"] + c["assertions"]["failed"] + c["assertions"]["skipped"]
            if red:
                entries[match["file"]] = {
                    "file": match["file"], "family": family(match["file"]),
                    "status": "runtime-gap", **c,
                }
            continue
        if match := TOTAL_RE.search(raw):
            total = counts(match)

    if total is None:
        raise SystemExit("spec3-baseline: TOTAL[3.0] line not found; refusing partial inventory")

    family_names = (
        "extended-constant-expressions", "tail-calls", "typed-function-references",
        "gc", "exception-handling", "multi-memory", "memory64", "table64",
        "relaxed-simd", "core-and-cross-cutting",
    )
    groups = {name: {
        "red_files": 0, "parser_failures": 0,
        "modules": {"passed": 0, "failed": 0, "skipped": 0},
        "assertions": {"passed": 0, "failed": 0, "skipped": 0},
        "gaps": {},
    } for name in family_names}
    for entry in entries.values():
        group = groups[entry["family"]]
        group["red_files"] += 1
        if entry["status"] == "parser-failure":
            group["parser_failures"] += 1
        else:
            add_counts(group, entry)

    document = {
        "schema": 2,
        "suite": {
            "repository": "WebAssembly/spec", "tag": "wg-3.0",
            "commit": "9d36019973201a19f9c9ebb0f10828b2fe2374aa", "wast_files": 258,
        },
        "tools": {
            "primary": {
                "name": "wast2json", "project": "WebAssembly/wabt", "version": "1.0.41",
                "linux_amd64_asset_sha256": "83f8122e924745fcd70636e3594bc01c4c47f2d4c8f3c63b5d70d3f83a482677",
            },
            "fallback": {
                "name": "wasm", "project": "WebAssembly/spec",
                "version": "3.0.0", "revision": "9d36019973201a19f9c9ebb0f10828b2fe2374aa",
            },
        },
        "command": "make spec3",
        "exit_code": args.exit_code,
        "result": "fail" if args.exit_code else "pass",
        "totals_excluding_parser_failures": total,
        "inventory": {
            "red_files": len(entries),
            "green_files": 258 - len(entries),
            "parser_failures": sum(e["status"] == "parser-failure" for e in entries.values()),
            "interpreter_fallbacks": len(fallbacks),
            "by_family": dict(sorted(groups.items())),
            "files": [entries[name] for name in sorted(entries)],
        },
    }
    args.output.parent.mkdir(parents=True, exist_ok=True)
    args.output.write_text(json.dumps(document, indent=2, sort_keys=True) + "\n")


if __name__ == "__main__":
    main()
