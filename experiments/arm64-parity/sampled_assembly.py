"""Inspect leaf PCs from Wago's Samply capture against its retained code image.

Run with a Python environment containing capstone. Weights are observations,
not CPU time; ancestor frames and samples outside the exact JIT symbol are ignored.
"""
import base64
import collections
import gzip
import json
import pathlib
import sys
import capstone

root = pathlib.Path(sys.argv[1])
events = json.loads((root / "images.json").read_text())
symbols = {}
for event in events:
    im = event.get("image")
    if not im:
        continue
    for region in im["regions"]:
        if region["kind"] not in ("guest-body", "entry-adapter"):
            continue
        symbol = f'wago:{im["module_id"]}:{im["artifact_id"]}:g{im["id"]}:f{region["function"]}:r{region["offset"]:x}:{region["kind"]}:{region["name"]}'
        symbols[symbol] = (im, region)
profile = json.load(gzip.open(root / "samply.json.gz"))
counts = collections.Counter()
for thread in profile["threads"]:
    for i, stack in enumerate(thread["samples"]["stack"]):
        if stack is None:
            continue
        frame = thread["stackTable"]["frame"][stack]
        function = thread["frameTable"]["func"][frame]
        symbol = thread["stringArray"][thread["funcTable"]["name"][function]]
        if symbol not in symbols:
            continue
        native = thread["frameTable"]["nativeSymbol"][frame]
        if native is None:
            continue
        # Samply stores addresses relative to its library. Use the exact
        # native symbol's start, then rebase onto Wago's matching region.
        delta = thread["frameTable"]["address"][frame] - thread["nativeSymbols"]["address"][native]
        im, region = symbols[symbol]
        if not 0 <= delta < region["size"]:
            raise ValueError("sample outside matching region")
        offset = region["offset"] + delta
        weight = thread["samples"].get("weight")
        counts[(symbol, offset)] += weight[i] if weight else 1
engine = capstone.Cs(capstone.CS_ARCH_ARM64, capstone.CS_MODE_LITTLE_ENDIAN)
for symbol, (im, region) in symbols.items():
    code = base64.b64decode(im["code"])
    relevant = [(offset, count) for (name, offset), count in counts.items() if name == symbol]
    if not relevant:
        continue
    total = sum(count for _, count in relevant)
    print(f'{symbol}: {total} leaf observations')
    decoded = {i.address: i for i in engine.disasm(code, 0)}
    for offset, count in sorted(relevant, key=lambda x: -x[1])[:25]:
        insn = decoded.get(offset)
        source = next((s for s in im.get("sources", []) if s["offset"] <= offset < s["offset"] + s["size"]), None)
        print(f'{count:5} ({count / total:5.1%}) +0x{offset:04x} {insn.mnemonic if insn else "?":8} {insn.op_str if insn else "?"}  Wasm {source.get("wasm_offset") if source else "?"}')
    print("\nAssembly for sampled range:")
    lo = min(offset for offset, _ in relevant) & ~3
    hi = max(offset for offset, _ in relevant) + 4
    for offset in range(lo, hi, 4):
        insn = decoded.get(offset)
        if insn:
            print(f'{counts[(symbol, offset)]:5} +0x{offset:04x} {insn.mnemonic:8} {insn.op_str}')
