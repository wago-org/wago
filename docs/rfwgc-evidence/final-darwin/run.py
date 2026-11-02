#!/usr/bin/env python3
"""Replay pinned correctness commands on Darwin ARM64; never benchmark."""
import argparse
import hashlib
import json
import os
from pathlib import Path
import platform
import subprocess
import sys

HERE = Path(__file__).resolve().parent
PLAN = json.loads((HERE / "plan.json").read_text())


def capture(argv, cwd):
    return subprocess.check_output(argv, cwd=cwd, text=True).strip()


def write(path, value):
    path.write_text(json.dumps(value, indent=2) + "\n")


def manifest(root):
    names = subprocess.check_output(["git", "ls-files", "-z"], cwd=root).decode().split("\0")
    return {name: hashlib.sha256((root / name).read_bytes()).hexdigest()
            for name in names if name and (root / name).is_file()}


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("source", type=Path)
    parser.add_argument("output", type=Path)
    parser.add_argument("--mode", choices=["ordinary", "wagodebug", "wago_regalloccheck"], required=True)
    args = parser.parse_args()
    # This precedes all test/build execution and output creation.
    if platform.system() != "Darwin" or platform.machine() != "arm64":
        parser.error("requires actual Darwin arm64; cross compilation and Linux QEMU do not qualify")
    root, out = args.source.resolve(), args.output.resolve()
    if root == out or root in out.parents:
        parser.error("output must be outside the immutable source checkout")
    if capture(["git", "rev-parse", "HEAD"], root) != PLAN["source"]:
        parser.error("source HEAD does not match the pinned source")
    if capture(["git", "status", "--porcelain", "--untracked-files=no"], root):
        parser.error("tracked source or submodules are dirty")
    if json.loads(capture(["go", "env", "-json", "GOOS", "GOARCH"], root)) != {"GOOS": "darwin", "GOARCH": "arm64"}:
        parser.error("Go target is not native Darwin ARM64")
    if capture(["go", "version"], root) != "go version go1.27.1 darwin/arm64":
        parser.error("requires pinned Go 1.27.1")
    if any(os.environ.get(k) for k in ["GOFLAGS", "WAGO_BOUNDS", "WAGO_SHARED_SCALAR"]):
        parser.error("clear inherited GOFLAGS/WAGO_BOUNDS/WAGO_SHARED_SCALAR")
    out.mkdir(parents=True, exist_ok=False)
    before = manifest(root)
    write(out / "source-before.json", before)
    write(out / "host.json", {"source": PLAN["source"], "uname": capture(["uname", "-a"], root),
          "sw_vers": capture(["sw_vers"], root), "go": capture(["go", "version"], root),
          "driver_sha256": hashlib.sha256(Path(__file__).read_bytes()).hexdigest(),
          "plan_sha256": hashlib.sha256((HERE / "plan.json").read_bytes()).hexdigest()})
    results = []
    for lane in [x for x in PLAN["lanes"] if x["mode"] == args.mode]:
        dest = out / lane["id"]
        dest.mkdir()
        env = os.environ.copy()
        env.update(lane["environment"])
        env.update(GOTOOLCHAIN="local", GOMAXPROCS="2")
        cwd = root / lane["cwd"]
        write(dest / "command.json", lane)
        # Record included/excluded source files for each tag configuration.
        argv = lane["argv"]
        stop = argv.index("-args") if "-args" in argv else len(argv)
        packages = [x for x in argv[:stop] if x.startswith("./")]
        tags = [x for x in argv if x.startswith("-tags=")]
        with (dest / "package-census.jsonl").open("w") as stream:
            census = subprocess.run(["go", "list", "-json", "-test"] + tags + packages,
                                    cwd=cwd, env=env, stdout=stream, stderr=subprocess.STDOUT)
        events, details, passes, failures = [], {}, 0, 0
        with (dest / "raw.jsonl").open("w") as stream:
            proc = subprocess.Popen(argv, cwd=cwd, env=env, stdout=subprocess.PIPE,
                                    stderr=subprocess.STDOUT, text=True)
            for line in proc.stdout:
                stream.write(line)
                stream.flush()
                try:
                    event = json.loads(line)
                except ValueError:
                    continue
                key = (event.get("Package"), event.get("Test"))
                if event.get("Action") == "output":
                    details.setdefault(key, []).append(event.get("Output", ""))
                if event.get("Action") == "skip":
                    events.append({"package": key[0], "test": key[1], "detail": "".join(details.get(key, []))})
                if event.get("Test") and event.get("Action") == "pass":
                    passes += 1
                if event.get("Action") == "fail":
                    failures += 1
            code = proc.wait()
        result = {"id": lane["id"], "exit_code": code, "census_exit_code": census.returncode,
                  "passing_test_events": passes, "failure_events": failures, "skips": events,
                  "status": "PASS_WITH_SKIPS" if code == 0 and passes and not failures and census.returncode == 0 and events else
                            "PASS" if code == 0 and passes and not failures and census.returncode == 0 else "FAIL"}
        write(dest / "result.json", result)
        results.append(result)
        write(out / "results.json", results)
        print(lane["id"], result["status"], flush=True)
    after = manifest(root)
    unchanged = before == after and not capture(["git", "status", "--porcelain", "--untracked-files=no"], root)
    write(out / "source-after.json", after)
    write(out / "summary.json", {"source": PLAN["source"], "source_unchanged": bool(unchanged),
          "native_execution": True, "skip_review_required": any(x["skips"] for x in results),
          "qualification": "REQUIRES_REVIEW", "results": results})
    return 0 if unchanged and all(x["status"] != "FAIL" for x in results) else 1


if __name__ == "__main__":
    sys.exit(main())
