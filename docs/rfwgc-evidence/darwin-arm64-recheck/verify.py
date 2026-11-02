#!/usr/bin/env python3
from pathlib import Path

root = Path(__file__).resolve().parent
rows = [line.split("\t") for line in (root / "results.tsv").read_text().splitlines()]
assert len(rows) == 7, rows
for mode in ("ordinary", "wagodebug", "wago_regalloccheck"):
    for lane in ("focused", "diagnostics"):
        assert [mode, lane, "0", "0"] in rows, (mode, lane, rows)
assert ["negative-original-call.go", "focused", "0", "1"] in rows
negative = (root / "logs" / "negative.log").read_text()
assert "direct Go import local-state join differs at local 0" in negative
assert "--- FAIL: TestDynamicImportUnwindLocalJoinARM64" in negative
for mode in ("ordinary", "wagodebug", "wago_regalloccheck"):
    for lane in ("focused", "diagnostics"):
        log = (root / "logs" / f"{mode}-{lane}.log").read_text()
        assert "\nFAIL\n" not in log and log.endswith("PASS\n"), (mode, lane)
print("PASS: six positive lanes and the original-call.go negative control")
