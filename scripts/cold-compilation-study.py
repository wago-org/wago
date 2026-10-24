#!/usr/bin/env python3
"""One alternating fresh-process pair per frozen input on a native Linux host."""
import argparse
import hashlib
import json
import math
import os
from pathlib import Path
import platform
import shutil
import subprocess
import tempfile

BASELINE = "dc13730184e8ba6a1a2b3e3887c53a2b880db88c"
MANIFEST = "docs/research/cold-compilation-2026-10-23/manifest.json"
HARNESS = "bench/suite/optimization_study_linux_test.go"


def command(args, cwd, env=None):
    return subprocess.check_output(args, cwd=cwd, env=env, text=True).strip()


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--output", type=Path, required=True)
    parser.add_argument("--file-backed-code", action="store_true")
    args = parser.parse_args()
    if platform.system() != "Linux":
        parser.error("native Linux is required")
    root = Path(__file__).resolve().parent.parent
    goos, goarch = command(["go", "env", "GOOS", "GOARCH"], root).splitlines()
    expected = {"aarch64": "arm64", "x86_64": "amd64"}.get(platform.machine())
    if goos != "linux" or goarch != expected:
        parser.error("Go target must match the native host")
    output = args.output.resolve()
    output.mkdir(parents=True, exist_ok=True)
    manifest = root / MANIFEST
    inputs = json.loads(manifest.read_text())["corpus"]
    if len(inputs) != 20 or len({row["id"] for row in inputs}) != 20:
        raise ValueError("expected the frozen 20 unique inputs")
    env = os.environ.copy()
    for key in list(env):
        if key.startswith("WAGO_STUDY_"):
            del env[key]
    env.update(GOMAXPROCS="1", WAGO_STUDY_MANIFEST=str(manifest), WAGO_STUDY_EXEC_TIME="30ms")
    cpu = min(os.sched_getaffinity(0))
    identity = {
        "baseline_sha": BASELINE,
        "candidate_sha": command(["git", "rev-parse", "HEAD"], root),
        "candidate_option": "file-backed-code" if args.file_backed_code else "default",
        "manifest_sha256": hashlib.sha256(manifest.read_bytes()).hexdigest(),
        "harness_sha256": hashlib.sha256((root / HARNESS).read_bytes()).hexdigest(),
        "baseline_harness": "copied unchanged from candidate; baseline production source unchanged",
        "go": command(["go", "version"], root),
        "uname": platform.uname()._asdict(),
        "cpu": command(["lscpu"], root),
        "filesystem": command(["findmnt", "-T", str(root)], root),
        "temporary_filesystem": command(["findmnt", "-T", tempfile.gettempdir()], root),
        "affinity_cpu": cpu,
        "protocol": "one pair per input, alternating order, fresh processes, one worker, GOMAXPROCS=1; all builds finish before timing",
    }
    (output / "identity.json").write_text(json.dumps(identity, indent=2) + "\n")
    with tempfile.TemporaryDirectory(prefix="wago-cold-study-") as temporary:
        temporary = Path(temporary)
        base = temporary / "baseline"
        subprocess.run(["git", "worktree", "add", "--detach", str(base), BASELINE], cwd=root, check=True)
        try:
            shutil.copyfile(root / HARNESS, base / HARNESS)
            binaries = {}
            for label, source in (("baseline", base), ("candidate", root)):
                binary = temporary / (label + ".test")
                subprocess.run(["go", "test", "-c", "-o", str(binary), "./suite"], cwd=source / "bench", env=env, check=True)
                binaries[label] = binary
                identity[label + "_binary_sha256"] = hashlib.sha256(binary.read_bytes()).hexdigest()
            (output / "identity.json").write_text(json.dumps(identity, indent=2) + "\n")
            samples = {"baseline": {}, "candidate": {}}
            with (output / "samples.jsonl").open("w") as receipt, (output / "raw.log").open("w") as log:
                for index, entry in enumerate(inputs):
                    order = ("baseline", "candidate") if index % 2 == 0 else ("candidate", "baseline")
                    for label in order:
                        sample_env = env.copy()
                        if label == "candidate" and args.file_backed_code:
                            sample_env.update(WAGO_STUDY_OPT="file-backed-code", WAGO_STUDY_ON="1")
                        result = subprocess.run(
                            [str(binaries[label]), "-test.run=^TestOptimizationStudySample$", "-test.v", "-test.timeout=3m", "-wago.corpus=" + entry["id"]],
                            cwd=root / "bench/suite", env=sample_env, text=True, stdout=subprocess.PIPE, stderr=subprocess.STDOUT,
                            preexec_fn=lambda: os.sched_setaffinity(0, {cpu}), timeout=200,
                        )
                        log.write(f"STUDY_IMAGE {label}\n{result.stdout}\n")
                        log.flush()
                        result.check_returncode()
                        rows = [json.loads(line.removeprefix("STUDY_SAMPLE ")) for line in result.stdout.splitlines() if line.startswith("STUDY_SAMPLE ")]
                        if len(rows) != 1 or rows[0]["id"] != entry["id"] or rows[0]["oracle"] != "pass":
                            raise ValueError("missing or invalid sample: " + entry["id"])
                        sample = rows[0]
                        sample["image"] = label
                        samples[label][entry["id"]] = sample
                        receipt.write(json.dumps(sample) + "\n")
                        receipt.flush()
                        print(label, entry["id"], sample["compile_ns"], sample["compile_peak_rss_bytes"], flush=True)
            baseline, candidate = samples["baseline"], samples["candidate"]
            metrics = ("compile_ns", "compile_peak_rss_bytes", "allocated_bytes", "native_code_bytes")
            summary = {
                "pairs": len(inputs), "oracles_passed": 2 * len(inputs),
                "native_hashes_equal": sum(candidate[key]["native_code_sha256"] == baseline[key]["native_code_sha256"] for key in baseline),
                "ratios_of_totals": {metric: sum(v[metric] for v in candidate.values()) / sum(v[metric] for v in baseline.values()) for metric in metrics},
                "execution_geomean_ratio": math.exp(sum(math.log(candidate[key]["exec_ns"] / baseline[key]["exec_ns"]) for key in baseline) / len(inputs)),
                "per_input_ratios": {key: {metric: candidate[key][metric] / baseline[key][metric] for metric in (*metrics, "exec_ns")} for key in baseline},
                "limitations": "Single screening pair on a shared GitHub runner; 30ms execution windows are noisy. Process RSS excludes nonresident file cache. Ratios of summed peaks do not describe a single process.",
            }
            (output / "summary.json").write_text(json.dumps(summary, indent=2) + "\n")
            print(json.dumps(summary, indent=2), flush=True)
        finally:
            subprocess.run(["git", "worktree", "remove", "--force", str(base)], cwd=root, check=True)


if __name__ == "__main__":
    main()
