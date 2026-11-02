#!/usr/bin/env python3
import hashlib
import json
import pathlib
import re
import subprocess

ROOT = pathlib.Path(__file__).resolve().parents[3]
OUT = pathlib.Path(__file__).resolve().parent
HARNESS = "8801252952255edf6ab6196250bd544873785328"
SOURCE = "7e41fafbbcd737ed1edce11b94b43b4eda134dcf"
WORKFLOW = ROOT / ".github/workflows/ci.yml"

def run(args, *, check=True):
    return subprocess.run(args, cwd=ROOT, text=True, capture_output=True, check=check)

def sha(path):
    return hashlib.sha256(path.read_bytes()).hexdigest()

def job_blocks(text):
    matches = list(re.finditer(r"(?m)^  ([a-zA-Z0-9_-]+):\n", text[text.index("jobs:\n") + 6:]))
    base = text.index("jobs:\n") + 6
    result = {}
    for i, match in enumerate(matches):
        start = base + match.start()
        end = base + (matches[i + 1].start() if i + 1 < len(matches) else len(text) - base)
        result[match.group(1)] = text[start:end]
    return result

def needs(block):
    m = re.search(r"(?m)^    needs: \[([^]]*)\]", block)
    return [] if not m else [x.strip() for x in m.group(1).split(",") if x.strip()]

current = WORKFLOW.read_text()
pinned = run(["git", "show", f"{HARNESS}:.github/workflows/ci.yml"]).stdout
assert current == pinned, "installed workflow differs from exact harness commit"
jobs = job_blocks(pinned)

native = jobs["darwin-tinygo-native"]
summary = jobs["darwin-tinygo-summary"]
aggregate = jobs["ci-ok"]
assert "mode: [ordinary, wagodebug, wago_regalloccheck]" in native
assert "timeout-minutes: 110" in native and "max-parallel: 1" in native
assert native.count("harness.py native") == 1
assert f'[[ "$EXPECTED_SOURCE" == {SOURCE} ]]' in native
assert '[[ "$EXPECTED_HARNESS" == "$GITHUB_SHA" ]]' in native
assert "tinygo0.42.0.darwin-arm64.tar.gz" in native
assert "493da3585e66c5da677a4be39402faf2041b4e45a61eb456e4547eca753a544e" in native
assert "go-version: '1.27.1'" in native
assert "TinyGo Darwin ARM64 (${{ matrix.mode }})" in native
assert "darwin-tinygo-${{ matrix.mode }}-${{ github.run_attempt }}" in native
assert "inputs.darwin_tinygo_supplement" in native
assert '[[ "$OTHER_DARWIN" == false && "$OTHER_ARM" == false ]]' in native

assert "darwin-tinygo-native" in needs(aggregate)
assert "darwin-tinygo-summary" in needs(aggregate)
assert "ci-ok" not in needs(summary)
assert 'n["darwin-tinygo-native"]["result"] == "success"' in summary
assert 'k != "darwin-tinygo-native"' in summary
assert 'v["result"] == "skipped"' in summary
assert "timeout-minutes: 3" in summary
assert "inputs.darwin_tinygo_supplement" in aggregate
assert "Full CI not run (qualification dispatch)" in aggregate
assert "'CI'" in aggregate

for isolated in ("changes", "native-arm64-rr3", "native-arm64-rr3-summary", "darwin-qualification", "windows-qualification", "darwin-qualification-summary"):
    assert "!inputs.darwin_tinygo_supplement" in jobs[isolated], isolated

graph = {name: needs(block) for name, block in jobs.items()}
active = set()
done = set()
def visit(name):
    if name in active:
        raise AssertionError(f"dependency cycle through {name}")
    if name in done:
        return
    active.add(name)
    for dep in graph[name]:
        assert dep in graph, (name, dep)
        visit(dep)
    active.remove(name)
    done.add(name)
for name in graph:
    visit(name)

assert "format('-darwin-tinygo-{0}', github.run_id)" in pinned
assert "cancel-in-progress: ${{ !(github.event_name == 'workflow_dispatch' && inputs.darwin_tinygo_supplement) }}" in pinned

plan = json.loads((ROOT / "docs/rfwgc-evidence/darwin-tinygo-supplement/suite-plan.json").read_text())
counts = {mode: sum(1 for names in packages.values() for name in names if "/" not in name) for mode, packages in plan.items()}
assert counts == {"ordinary": 33, "wagodebug": 36, "wago_regalloccheck": 33}, counts

unchanged = run(["git", "diff", "--name-only", f"{HARNESS}^", HARNESS, "--", "docs/rfwgc-evidence/darwin-tinygo-supplement"]).stdout
assert unchanged == "", unchanged
post_change = run(["git", "diff", "--name-only", HARNESS, "HEAD", "--", "docs/rfwgc-evidence/darwin-tinygo-supplement", ".github/workflows/ci.yml", "tests/integration/cipolicy"]).stdout
assert post_change == "", post_change

toolchain = json.loads((ROOT / "docs/rfwgc-evidence/darwin-tinygo-supplement/toolchain.json").read_text())
assert toolchain["source"] == SOURCE
assert toolchain["archive_sha256"] == "493da3585e66c5da677a4be39402faf2041b4e45a61eb456e4547eca753a544e"
assert toolchain["compiler_sha256"] == "1b74ee7ef9841f01364823be1254833a96832e74076957c956d89872f705b7d2"

# Execute the exact summary predicate against one positive and representative
# negative maps. The negatives must all fail, preserving failure propagation.
summary_code = 'import json,os; n=json.loads(os.environ["RESULTS"]); assert n["darwin-tinygo-native"]["result"] == "success", n; assert all(v["result"] == "skipped" for k,v in n.items() if k != "darwin-tinygo-native"), n'
assert summary_code in summary
cases = {
    "positive": ({"darwin-tinygo-native": {"result": "success"}, "changes": {"result": "skipped"}}, 0),
    "native_failure": ({"darwin-tinygo-native": {"result": "failure"}, "changes": {"result": "skipped"}}, 1),
    "native_cancelled": ({"darwin-tinygo-native": {"result": "cancelled"}, "changes": {"result": "skipped"}}, 1),
    "unexpected_normal_success": ({"darwin-tinygo-native": {"result": "success"}, "changes": {"result": "success"}}, 1),
    "unexpected_normal_failure": ({"darwin-tinygo-native": {"result": "success"}, "lint": {"result": "failure"}}, 1),
}
controls = {}
for name, (payload, expected) in cases.items():
    p = subprocess.run(["python3", "-c", summary_code], env={"RESULTS": json.dumps(payload)}, text=True, capture_output=True)
    actual = 0 if p.returncode == 0 else 1
    assert actual == expected, (name, p.returncode, p.stderr)
    controls[name] = {"expected_class": expected, "returncode": p.returncode}

result = {
    "activation": "YES",
    "native_execution": "NOT_RUN",
    "harness": HARNESS,
    "source": SOURCE,
    "workflow_sha256": sha(WORKFLOW),
    "harness_py_sha256": sha(ROOT / "docs/rfwgc-evidence/darwin-tinygo-supplement/harness.py"),
    "suite_plan_sha256": sha(ROOT / "docs/rfwgc-evidence/darwin-tinygo-supplement/suite-plan.json"),
    "toolchain_sha256": sha(ROOT / "docs/rfwgc-evidence/darwin-tinygo-supplement/toolchain.json"),
    "census": counts,
    "job_minutes": {"native_matrix": 330, "summary": 3, "total": 333},
    "summary_controls": controls,
    "graph_jobs": len(graph),
    "native_harness_invocations_per_matrix_cell": 1,
    "gaps": ["native Darwin ARM64 TinyGo execution has not run", "5b02ec67afe0dce181a7f8d508e270c7ecb0ecfc remains outside the execution source pin", "this is activation review, not final platform approval"],
}
(OUT / "review.json").write_text(json.dumps(result, indent=2, sort_keys=True) + "\n")
print("PASS activation YES; exact pins verified; native NOT_RUN")
