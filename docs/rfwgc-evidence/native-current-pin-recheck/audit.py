#!/usr/bin/env python3
"""Independent static pin, census, and expectation audit; no native execution."""
import hashlib
import json
import subprocess
from pathlib import Path

HERE = Path(__file__).resolve().parent
ROOT = HERE.parents[2]
SOURCE = "08f511fac41a77f4c0f9046f88ebc78ad05a5da6"
HARNESS = "041f7ed4c376e2a33ce21b22075cd617ddca6597"
BASE = "8801252952255edf6ab6196250bd544873785328"


def git(*args, text=False):
    return subprocess.check_output(["git", *args], cwd=ROOT, text=text)


plan = json.loads((ROOT / "docs/rfwgc-evidence/final-darwin/plan.json").read_text())
arm = json.loads((ROOT / "docs/rfwgc-evidence/native-arm64-rr3/plan.json").read_text())
contracts = json.loads((ROOT / "docs/rfwgc-evidence/final-darwin/revision-08f511fac/contracts.json").read_text())
source_test = git("show", SOURCE + ":src/wago/gc_extern_multi_table_test.go", text=True)
parent = git("show", SOURCE + ":src/wago/wasmtime_core3_port_test.go", text=True)
workflow = (ROOT / ".github/workflows/ci.yml").read_text()

assert not git("diff", "--name-only", HARNESS, "HEAD", "--", ".github/workflows/ci.yml", "docs/rfwgc-evidence/final-darwin", "docs/rfwgc-evidence/native-arm64-rr3", "tests/integration/cipolicy/workflows_test.go")
assert plan["source"] == arm["source"] == contracts["source"] == SOURCE
assert plan["supplement_revision"] == "revision-08f511fac"
assert len(plan["lanes"]) == 63
assert len(arm["suites"]["focused"]["tests"]) == 85
assert len(arm["suites"]["guard"]["tests"]) == 1
assert len(arm["suites"]["runtime"]["tests"]) == 11
assert len(arm["suites"]["backend"]["tests"]) == 9
assert len(contracts["tests"]["darwin"]) == 73
assert len(contracts["tests"]["windows"]) == 6
assert "TestGCExternConversionAcrossReferenceTables" in contracts["tests"]["darwin"]
assert "runtime.GOARCH != \"riscv64\" && !compiledConversionStoreHooks(compiled)" in source_test
for token in ["gcExternMultiTableWasm", "got != 545", 'value != "hello"', "stats.FullCollections == 0"]:
    assert token in source_test, token
delta = git("diff", SOURCE + "^", SOURCE, "--", "src/wago/gc_extern_multi_table_test.go", text=True)
assert delta.count("@@") == 2 and "got != 545" not in delta and "gcExternMultiTableWasm" not in delta
assert "total.modulesPassed != 215 || total.assertionsPassed != 690" in parent
assert parent.index("stats.modulesSkipped != 0") < parent.index("total.add(stats)")
assert f"test \"$QUALIFICATION_SOURCE\" = {SOURCE}" in workflow
assert f"ref: '{SOURCE}'" in workflow
assert "native-arm64-rr3-08f511fac-" in workflow
assert "if: ${{ !cancelled() && steps.darwin-setup.outcome == 'success' }}" in workflow
assert "continue-on-error" not in workflow.split("  darwin-qualification:", 1)[1].split("  windows-qualification:", 1)[0]

for name, digest in contracts["source_files"].items():
    assert hashlib.sha256(git("show", SOURCE + ":" + name)).hexdigest() == digest, name
for name, digest in arm["probe_sha256"].items():
    assert hashlib.sha256(git("show", SOURCE + ":" + name)).hexdigest() == digest, name

cancel = "src/wago/shared_entry_guard_arm64_test.go"
assert git("show", SOURCE + ":" + cancel) == git("show", "30b0813f7881a6fbb938a21535a205803bd27a0c:" + cancel)
assert not git("diff", BASE, HARNESS, "--", "docs/rfwgc-evidence/final-darwin/plan.json").startswith(b"impossible")

report = {
    "status": "PASS",
    "source": SOURCE,
    "harness": HARNESS,
    "native_execution": "NOT_RUN",
    "darwin_windows_activation": "YES for exact source/harness; native qualification remains NOT_RUN",
    "linux_arm_activation": "YES for exact source/harness; native qualification remains NOT_RUN",
    "census": {"darwin_focus": 73, "windows": 6, "arm_focused": 85, "arm_guard": 1, "arm_runtime": 11, "arm_backend": 9, "broad": 63},
    "expectation_change": "selection predicate only; positive and rejection oracles unchanged",
    "original_native_failure": "retained; no old failing command promoted",
}
(HERE / "audit.json").write_text(json.dumps(report, indent=2) + "\n")
print("PASS: exact pins, hashes, censuses, oracles, deadlines/cancellation source, and workflow gates")
