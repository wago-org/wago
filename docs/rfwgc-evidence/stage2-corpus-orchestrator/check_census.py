#!/usr/bin/env python3
"""Cross-check completed compiler census outputs against the entire catalog."""
import collections
import json
import pathlib

ROOT = pathlib.Path(__file__).resolve().parents[3]
EVIDENCE = pathlib.Path(__file__).resolve().parent.parent / "stage2-corpus"
catalog = json.loads((ROOT / "corpus/catalog.json").read_text())["benchmarks"]
unique = {}
weighted = collections.Counter()
for entry in catalog:
    lines = (EVIDENCE / f"static-{entry['id']}.txt").read_text().splitlines()
    reports = [json.loads(line.removeprefix("CORPUS_STATS "))
               for line in lines if line.startswith("CORPUS_STATS ")]
    assert len(reports) == 1, entry["id"]
    report = reports[0]
    functions = report["Report"]["Functions"]
    counts = {
        "functions": report["Functions"],
        "analyzed": len(functions),
        "omitted": report["Report"]["Omitted"],
        "analyzed_denominator_unknown": sum(not f["DenominatorKnown"] for f in functions),
        "proven_sites": sum(f["Sites"] for f in functions if f["Candidate"]),
        "finalized_scalar_replacements": report["Lowered"],
    }
    assert counts["functions"] == counts["analyzed"] + counts["omitted"]
    weighted.update(counts)
    digest = entry["artifact_sha256"]
    if digest in unique:
        assert unique[digest] == counts, entry["id"]
    unique[digest] = counts

deduplicated = collections.Counter()
for counts in unique.values():
    deduplicated.update(counts)
outcomes = json.loads((EVIDENCE / "static-outcomes.json").read_text())
assert len(outcomes) == len(catalog)
assert {r["id"] for r in outcomes} == {r["id"] for r in catalog}
assert all(row["returncode"] == 0 for row in outcomes)
print(json.dumps({"compiled_catalog_entries": len(outcomes),
                  "unique_binaries": len(unique),
                  "catalog_weighted": dict(weighted),
                  "unique_binary": dict(deduplicated)}, indent=2))
