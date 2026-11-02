#!/usr/bin/env python3
"""Fail-fast all-mode qualification of the supported QEMU RT signal map."""
import gzip
import hashlib
import json
import os
import re
import signal
import subprocess
import tempfile
from pathlib import Path

HERE = Path(__file__).resolve().parent
DIAG = HERE.parent / "qemu-arm64-final-diagnosis"
MAP = "32 36 28,64 64 1"
RUN = r"^(TestPlatformExecutionArchitecture|TestRR.*|TestSharedEntryGuardCancellationAndGC|TestDesign.*|TestR9H2.*|TestGCExternConversion.*|TestRV64ManagedFunctionJoint.*|TestRV64CompactExtern.*|TestRV64CompactConvertedExternShellReclamation|TestRV64CompactForeignExternPinAndReclaim)$"
TIMEOUT = 15 * 60


def sha(path):
    h = hashlib.sha256()
    with open(path, "rb") as f:
        for block in iter(lambda: f.read(1024 * 1024), b""):
            h.update(block)
    return h.hexdigest()


def write(name, value):
    (HERE / name).write_text(json.dumps(value, indent=2, sort_keys=True) + "\n")


def main():
    resume = (HERE / "results.json").exists()
    if any(HERE.iterdir()) and not resume:
        allowed = {HERE / "run.py", HERE / "platform_arch_test.go"}
        if set(HERE.iterdir()) != allowed:
            raise SystemExit("STOP: output directory is not pristine")
    provenance = json.loads((DIAG / "provenance.json").read_text())
    if provenance["pin"] != "08f511fac41a77f4c0f9046f88ebc78ad05a5da6" or provenance["signal_map"] != MAP:
        raise SystemExit("STOP: diagnosis provenance/map mismatch")
    tools = {
        "qemu": {"path": provenance["qemu"], "expected": provenance["qemu_sha256"]},
        "go": {"path": provenance["go"], "expected": provenance["go_sha256"]},
    }
    for item in tools.values():
        item["actual"] = sha(item["path"])
        if item["actual"] != item["expected"]:
            raise SystemExit("STOP: tool hash mismatch: " + item["path"])
    write("tools-before.json", tools)

    manifest = json.loads((DIAG / "source-manifest.json").read_text())
    source = Path(provenance["export"])
    bad = []
    for rel, expected in manifest.items():
        path = source / rel
        if not path.is_file() or sha(path) != expected:
            bad.append(rel)
    source_check = {"pin": provenance["pin"], "entries": len(manifest), "mismatches": bad}
    write("source-before.json", source_check)
    if bad:
        raise SystemExit("STOP: source integrity mismatch")

    env = dict(provenance["environment"])
    env["WAGO_EXPECT_ARCH"] = "arm64"
    overlay = {"Replace": {
        str(source / "src/wago/shared_entry_guard_arm64_test.go"): str(Path(provenance["export"]).parent / "shared_entry_guard_arm64_test.go"),
        str(source / "src/wago/platform_arch_test.go"): str(HERE / "platform_arch_test.go"),
    }}
    write("overlay.json", overlay)
    if resume:
        binaries = json.loads((HERE / "binaries.json").read_text())
        for mode, item in binaries.items():
            if not Path(item["path"]).is_file() or sha(item["path"]) != item["sha256"]:
                raise SystemExit("STOP: resume binary mismatch in " + mode)
    else:
        scratch = Path(tempfile.mkdtemp(prefix="wago-qemu-map-recheck-", dir="/home/dev/.cache"))
        overlay_path = scratch / "overlay.json"
        overlay_path.write_text(json.dumps(overlay))
        binaries = {}
        for mode, tag in (("ordinary", None), ("wagodebug", "wagodebug"), ("wago_regalloccheck", "wago_regalloccheck")):
            binary = scratch / (mode + ".test")
            argv = [provenance["go"], "test", "-c", "-p=2", "-overlay", str(overlay_path)]
            if tag:
                argv.append("-tags=" + tag)
            argv += ["-o", str(binary), "./src/wago"]
            build_log = HERE / (mode + "-build.log")
            with build_log.open("wb") as out:
                result = subprocess.run(argv, cwd=source, env=env, stdout=out, stderr=subprocess.STDOUT, timeout=240)
            if result.returncode:
                raise SystemExit("STOP: build failure in " + mode)
            binaries[mode] = {"path": str(binary), "sha256": sha(binary), "argv": argv}
        write("binaries.json", binaries)
    rows = json.loads((HERE / "results.json").read_text()) if resume else []
    expected_names = None
    for mode, binary in binaries.items():
        list_argv = [provenance["qemu"], "-cpu", "max", "-t", MAP, binary["path"], "-test.list", RUN]
        listed = subprocess.run(list_argv, cwd=source / "src/wago", env=env, stdout=subprocess.PIPE,
                                stderr=subprocess.STDOUT, timeout=60, check=False, text=True)
        names = [x for x in listed.stdout.splitlines() if x.startswith("Test")]
        if listed.returncode or len(names) != 76 or len(set(names)) != 76:
            write("results.json", rows + [{"mode": mode, "phase": "list", "exit": listed.returncode, "names": names}])
            raise SystemExit("STOP: selected-test census mismatch")
        if expected_names is None:
            expected_names = names
            write("census.json", {"regex": RUN, "count": 76, "names": names})
        elif names != expected_names:
            raise SystemExit("STOP: mode test census differs")

        prior = next((row for row in rows if row.get("mode") == mode and row.get("exit") == 0 and
                      len(row.get("passes", [])) == 76 and not row.get("failures") and not row.get("skips")), None)
        if prior:
            raw = HERE / (mode + ".log")
            compressed = Path(str(raw) + ".gz")
            if raw.exists() and not compressed.exists():
                with raw.open("rb") as src, gzip.GzipFile(filename=str(compressed), mode="wb", mtime=0) as dst:
                    dst.write(src.read())
                prior["compressed_sha256"] = sha(compressed)
                raw.unlink()
                write("results.json", rows)
            if not compressed.exists() or sha(compressed) != prior.get("compressed_sha256"):
                raise SystemExit("STOP: completed-cell archive mismatch in " + mode)
            continue

        argv = [provenance["qemu"], "-cpu", "max", "-t", MAP, binary["path"], "-test.v",
                "-test.count=1", "-test.timeout=12m", "-test.run=" + RUN]
        raw = HERE / (mode + ".log")
        with raw.open("wb") as out:
            child = subprocess.Popen(argv, cwd=source / "src/wago", env=env, stdout=out,
                                     stderr=subprocess.STDOUT, start_new_session=True)
            try:
                rc = child.wait(timeout=TIMEOUT)
            except subprocess.TimeoutExpired:
                os.killpg(child.pid, signal.SIGKILL)
                child.wait()
                rc = "TIMEOUT-KILLED"
        text = raw.read_text(errors="replace")
        passes = re.findall(r"^--- PASS: (Test\S+)", text, re.M)
        failures = re.findall(r"^--- FAIL: (Test\S+)", text, re.M)
        skips = re.findall(r"^--- SKIP: (Test\S+)", text, re.M)
        row = {"mode": mode, "argv": argv, "cwd": str(source / "src/wago"), "exit": rc,
               "pass_count": len(passes), "passes": passes, "failures": failures, "skips": skips,
               "log_sha256": sha(raw)}
        rows.append(row)
        write("results.json", rows)
        if rc != 0 or passes != expected_names or failures or skips:
            raise SystemExit("STOP: unexpected qualification failure in " + mode)
        with raw.open("rb") as src, gzip.GzipFile(filename=str(raw) + ".gz", mode="wb", mtime=0) as dst:
            dst.write(src.read())
        row["compressed_sha256"] = sha(str(raw) + ".gz")
        raw.unlink()
        write("results.json", rows)

    bad_after = [rel for rel, expected in manifest.items()
                 if not (source / rel).is_file() or sha(source / rel) != expected]
    write("source-after.json", {"pin": provenance["pin"], "entries": len(manifest), "mismatches": bad_after})
    tools_after = {k: {**v, "after": sha(v["path"])} for k, v in tools.items()}
    write("tools-after.json", tools_after)
    binaries_after = {mode: {"path": item["path"], "expected": item["sha256"], "after": sha(item["path"])}
                      for mode, item in binaries.items()}
    write("binaries-after.json", binaries_after)
    if (bad_after or any(v["after"] != v["expected"] for v in tools_after.values()) or
            any(v["after"] != v["expected"] for v in binaries_after.values())):
        raise SystemExit("STOP: post-run integrity failure")
    owned = []
    needle = str(HERE)
    for p in Path("/proc").iterdir():
        if p.name.isdigit():
            try:
                cmd = (p / "cmdline").read_bytes().replace(b"\0", b" ").decode(errors="replace")
                if needle in cmd and int(p.name) != os.getpid():
                    owned.append({"pid": int(p.name), "argv": cmd})
            except (OSError, ValueError):
                pass
    write("process-drain.json", {"owned_processes": owned, "method": "proc argv contains owned evidence path"})
    if owned:
        raise SystemExit("STOP: owned processes remain")
    write("verdict.json", {"map_activation_scope": True, "whole_76_all_3": True,
                            "cells": 228, "passes": 228, "failures": 0, "skips": 0,
                            "scope_limit": "guest signal 60 is sacrificed; not general RT-signal or full-port approval"})
    integrity = {p.name: sha(p) for p in sorted(HERE.iterdir())
                 if p.is_file() and p.name != "integrity.json"}
    write("integrity.json", integrity)


if __name__ == "__main__":
    main()
