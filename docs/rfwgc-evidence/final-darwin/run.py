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

from identity import validate
import discriminators

HERE = Path(__file__).resolve().parent
PLAN = json.loads((HERE / "plan.json").read_text())


def capture(argv, cwd):
    return subprocess.check_output(argv, cwd=cwd, text=True).strip()


def write(path, value):
    path.write_text(json.dumps(value, indent=2) + "\n")


def manifest(root):
    names = subprocess.check_output(["git", "ls-files", "--recurse-submodules", "-z"], cwd=root).decode().split("\0")
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
    identity = validate(os.environ.get("QUALIFICATION_SOURCE", ""),
                        os.environ.get("QUALIFICATION_DRIVER", ""), os.environ.get("QUALIFICATION_HARNESS", ""))
    root, out = args.source.resolve(), args.output.resolve()
    harness = Path(capture(["git", "rev-parse", "--show-toplevel"], HERE)).resolve()
    if root == harness or root in harness.parents or harness in root.parents:
        parser.error("source and harness must be independent checkouts")
    if subprocess.run(["sysctl", "-in", "sysctl.proc_translated"], capture_output=True, text=True).stdout.strip() == "1":
        parser.error("Rosetta translation is not native ARM64")
    if root == out or root in out.parents:
        parser.error("output must be outside the immutable source checkout")
    if capture(["git", "rev-parse", "HEAD"], root) != PLAN["source"]:
        parser.error("source HEAD does not match the pinned source")
    if capture(["git", "status", "--porcelain", "--untracked-files=all"], root):
        parser.error("tracked source or submodules are dirty")
    submodules = capture(["git", "submodule", "status", "--recursive"], root)
    if not submodules or any(line.startswith(("-", "+", "U")) for line in submodules.splitlines()):
        parser.error("pinned submodules must be initialized and unchanged")
    if json.loads(capture(["go", "env", "-json", "GOOS", "GOARCH"], root)) != {"GOOS": "darwin", "GOARCH": "arm64"}:
        parser.error("Go target is not native Darwin ARM64")
    if capture(["go", "version"], root) != "go version go1.27.1 darwin/arm64":
        parser.error("requires pinned Go 1.27.1")
    if any(os.environ.get(k) for k in ["GOFLAGS", "GOOS", "GOARCH", "GOWORK", "GOEXPERIMENT"]) or any(k.startswith("WAGO_") and k not in ("WAGO_WAST2JSON", "WAGO_SPEC_INTERPRETER", "WAGO_SPEC_INTERPRETER_REVISION") and v for k, v in os.environ.items()):
        parser.error("clear inherited target/build/test overrides; only setup interpreter variables are allowed")
    out.mkdir(parents=True, exist_ok=False)
    from contracts import darwin_overlays
    write(out/'overlay.json', {'Replace': darwin_overlays(root)})
    driver_before = manifest(harness)
    lanes = discriminators.lanes(args.mode) + [x for x in PLAN["lanes"] if x["mode"] == args.mode]
    if not lanes:
        parser.error("no planned lanes")
    write(out / "execution-plan.json", {"lanes": lanes, "unmet_gaps": PLAN["unmet_gaps"]})
    catalog = json.loads((root / "corpus/catalog.json").read_text())
    write(out / "catalog-platform-census.json", [
        {"id": entry["id"], "platforms": (entry.get("command") or {}).get("platforms", []),
         "command_platform_excluded": bool((entry.get("command") or {}).get("platforms")) and
             "darwin/arm64" not in entry["command"]["platforms"],
         "captured_reference": (entry.get("command") or {}).get("reference_runtime", "")}
        for entry in catalog["benchmarks"]])
    before = manifest(root)
    write(out / "source-before.json", before)
    write(out / "host.json", {**identity, "submodules": submodules, "uname": capture(["uname", "-a"], root),
          "sw_vers": capture(["sw_vers"], root), "go": capture(["go", "version"], root),
          "driver_sha256": hashlib.sha256(Path(__file__).read_bytes()).hexdigest(),
          "plan_sha256": hashlib.sha256((HERE / "plan.json").read_bytes()).hexdigest()})
    results = []
    for lane in lanes:
        dest = out / lane["id"]
        dest.mkdir()
        write(out / "progress.json", {"active_lane": lane["id"], "completed": len(results), "planned": len(lanes)})
        env = os.environ.copy()
        env.update({k: v.replace("{source}", str(root)) for k, v in lane["environment"].items()})
        env.update(GOTOOLCHAIN="local", GOMAXPROCS="2", GOWORK="off")
        cwd = root / lane["cwd"]
        write(dest / "command.json", {**lane, "effective_environment": {k: v for k, v in env.items() if k.startswith("WAGO_") or k in ("GOTOOLCHAIN", "GOMAXPROCS", "GOWORK", "GOROOT", "GOENV", "CGO_ENABLED")}})
        # Record included/excluded source files for each tag configuration.
        argv = lane["argv"]
        # Apply declared test-only migration to every original broad command.
        argv = argv[:2] + ["-mod=readonly", "-overlay="+str(out/"overlay.json")] + argv[2:]
        write(dest / "effective-argv.json", argv)
        stop = argv.index("-args") if "-args" in argv else len(argv)
        packages = [x for x in argv[:stop] if x.startswith("./")]
        tags = [x for x in argv if x.startswith("-tags=") or x == "-race"]
        with (dest / "package-census.jsonl").open("w") as stream:
            census = subprocess.run(["go", "list", "-mod=readonly", "-overlay="+str(out/"overlay.json"), "-json", "-test"] + tags + packages,
                                    cwd=cwd, env=env, stdout=stream, stderr=subprocess.STDOUT)
        events, failed, details, passes, failures = [], [], {}, 0, 0
        all_events = []
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
                if lane.get("discriminator"):
                    all_events.append(event)
                key = (event.get("Package"), event.get("Test"))
                if event.get("Action") == "output":
                    details.setdefault(key, []).append(event.get("Output", ""))
                if event.get("Action") == "skip":
                    events.append({"package": key[0], "test": key[1], "detail": "".join(details.get(key, []))})
                if event.get("Test") and event.get("Action") == "pass":
                    passes += 1
                if event.get("Action") == "fail":
                    failures += 1
                    failed.append({"package": key[0], "test": key[1], "detail": "".join(details.get(key, []))})
            code = proc.wait()
        result = {"id": lane["id"], "exit_code": code, "census_exit_code": census.returncode,
                  "passing_test_events": passes, "failure_events": failures, "failures": failed, "skips": events,
                  "status": "PASS_WITH_SKIPS" if code == 0 and passes and not failures and census.returncode == 0 and events else
                            "PASS" if code == 0 and passes and not failures and census.returncode == 0 else "FAIL"}
        if lane.get("discriminator"):
            fixture_dir = root / "tests/corpus/regressions/wasmtime-core3/core/gc/const-expr-gc"
            commands = json.loads((fixture_dir / "commands.json").read_text())
            if lane["discriminator"] == "core3-child":
                write(dest / "commands-source.json", commands)
                write(dest / "fixture-manifest.json", {p.name: hashlib.sha256(p.read_bytes()).hexdigest()
                      for p in fixture_dir.iterdir() if p.is_file()})
            diagnostic, exact = discriminators.evaluate(lane, all_events, commands)
            result["diagnostic"] = diagnostic
            if not exact:
                result["status"] = "FAIL"
        write(dest / "result.json", result)
        results.append(result)
        write(out / "results.json", results)
        print(lane["id"], result["status"], flush=True)
    after = manifest(root)
    unchanged = before == after and not capture(["git", "status", "--porcelain", "--untracked-files=no"], root)
    driver_unchanged = driver_before == manifest(harness)
    write(out / "driver-manifest.json", driver_before)
    write(out / "source-after.json", after)
    write(out / "summary.json", {"source": PLAN["source"], "source_unchanged": bool(unchanged),
          "native_execution": True, "driver_unchanged": driver_unchanged, "unmet_gaps": PLAN["unmet_gaps"], "skip_review_required": any(x["skips"] for x in results),
          "qualification": "REQUIRES_REVIEW", "results": results})
    return 0 if unchanged and driver_unchanged and len(results) == len(lanes) and all(x["status"] != "FAIL" for x in results) else 1


if __name__ == "__main__":
    sys.exit(main())
