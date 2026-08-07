#!/usr/bin/env python3
"""Generate replayable WAST command artifacts with a pinned wasm-tools parser.

This is the parser-independent fallback for upstream WAST files that the pinned
WebAssembly/spec interpreter cannot translate. The upstream source is copied
byte-for-byte; wasm-tools only supplies binary module artifacts and the command
graph. Output is normalized to the strict shape consumed by Wago's spectest
runner and corpus validator.
"""

import argparse
import json
import shutil
import subprocess
import tempfile
from pathlib import Path

WASM_TOOLS_VERSION = "wasm-tools 1.251.0 (a1a178a02 2026-05-28)"
DOCUMENT_SOURCE = "wasm-tools 1.251.0 json-from-wast"

REF_TYPES = {
    "anyref": "any",
    "eqref": "eq",
    "i31ref": "i31",
    "structref": "struct",
    "arrayref": "array",
    "exnref": "exn",
}


def normalize_value_tree(value):
    if isinstance(value, dict):
        value.pop("module_type", None)
        typ = value.get("type")
        if typ in REF_TYPES:
            value["type"] = "ref"
            value["heap_type"] = REF_TYPES[typ]
        for child in value.values():
            normalize_value_tree(child)
    elif isinstance(value, list):
        for child in value:
            normalize_value_tree(child)


def generate(source: Path, output: Path, wasm_tools: str):
    version = subprocess.run(
        [wasm_tools, "--version"], check=True, text=True, capture_output=True
    ).stdout.strip()
    if version != WASM_TOOLS_VERSION:
        raise SystemExit(
            f"unexpected wasm-tools version {version!r}; want {WASM_TOOLS_VERSION!r}"
        )

    output.mkdir(parents=True, exist_ok=True)
    with tempfile.TemporaryDirectory(prefix="wago-wast-json-") as tmp_name:
        tmp = Path(tmp_name)
        raw_json = tmp / "commands.json"
        subprocess.run(
            [
                wasm_tools,
                "json-from-wast",
                "--pretty",
                "--wasm-dir",
                str(tmp),
                "-o",
                str(raw_json),
                str(source),
            ],
            check=True,
        )
        document = json.loads(raw_json.read_text())
        commands = document.get("commands")
        if not isinstance(commands, list) or not commands:
            raise SystemExit("wasm-tools emitted an empty command graph")

        artifact = 0

        def copy_artifacts(command_list):
            nonlocal artifact
            for command in command_list:
                name = command.get("filename")
                if name:
                    source_artifact = tmp / name
                    if source_artifact.suffix != ".wasm" or not source_artifact.is_file():
                        raise SystemExit(f"unsupported wasm-tools artifact {name!r}")
                    canonical = f"commands.{artifact}.wasm"
                    artifact += 1
                    shutil.copyfile(source_artifact, output / canonical)
                    command["filename"] = canonical
                nested = command.get("commands")
                if nested is not None:
                    if command.get("type") != "thread" or not isinstance(nested, list):
                        raise SystemExit("nested command graph is not a thread")
                    copy_artifacts(nested)

        copy_artifacts(commands)

        document = {"commands": commands, "source": DOCUMENT_SOURCE}
        normalize_value_tree(document)
        (output / "commands.json").write_text(
            json.dumps(document, indent=2, sort_keys=True) + "\n"
        )
        shutil.copyfile(source, output / "source.wast")


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("source", type=Path)
    parser.add_argument("output", type=Path)
    parser.add_argument("--wasm-tools", default="wasm-tools")
    args = parser.parse_args()
    generate(args.source, args.output, args.wasm_tools)


if __name__ == "__main__":
    main()
