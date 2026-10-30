#!/usr/bin/env python3
"""Independent catalog/hash/type-section audit; not an execution benchmark.

This deliberately accepts only plain function type sections. A new composite
type stops the audit rather than guessing its extent or claiming no WasmGC.
The production decoder/compiler census supplies the authoritative compile result.
"""
import collections
import hashlib
import json
import pathlib

ROOT = pathlib.Path(__file__).resolve().parents[3]


def uleb(data, offset):
    value = shift = 0
    while True:
        byte = data[offset]
        offset += 1
        value |= (byte & 127) << shift
        if not byte & 128:
            return value, offset
        shift += 7
        if shift > 35:
            raise ValueError("oversized length/index")


def function_type_count(data):
    assert data[:8] == b"\x00asm\x01\x00\x00\x00"
    offset = 8
    while offset < len(data):
        section = data[offset]
        size, start = uleb(data, offset + 1)
        offset = start + size
        assert offset <= len(data)
        if section != 1:
            continue
        body = data[start:offset]
        count, pos = uleb(body, 0)
        for _ in range(count):
            assert body[pos] == 0x60, "non-function type: use production decoder"
            pos += 1
            for _ in range(2):  # parameter and result vectors
                length, pos = uleb(body, pos)
                for _ in range(length):
                    tag = body[pos]
                    pos += 1
                    if tag in (0x63, 0x64):
                        _, pos = uleb(body, pos)  # heap-type encoding extent
        assert pos == len(body)
        return count
    return 0


catalog_bytes = (ROOT / "corpus/catalog.json").read_bytes()
catalog = json.loads(catalog_bytes)
rows = []
unique = {}
for entry in catalog["benchmarks"]:
    path = ROOT / "corpus" / entry["artifact"]
    data = path.read_bytes()
    digest = hashlib.sha256(data).hexdigest()
    assert digest == entry["artifact_sha256"], entry["id"]
    count = function_type_count(data)
    unique[digest] = count
    rows.append({"id": entry["id"], "artifact": entry["artifact"],
                 "sha256": digest, "function_types": count,
                 "declared_struct_or_array_types": 0})
print(json.dumps({
    "catalog_sha256": hashlib.sha256(catalog_bytes).hexdigest(),
    "workload_entries": len(rows), "available_entries": len(rows),
    "unique_binaries": len(unique), "unique_function_types": sum(unique.values()),
    "declared_struct_or_array_types": 0,
    "workload_kinds": dict(collections.Counter(
        "command" if entry.get("command") else
        "semantic" if entry.get("semantic_exec") else "export"
        for entry in catalog["benchmarks"])),
    "rows": rows,
}, indent=2))
