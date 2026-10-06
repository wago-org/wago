#!/usr/bin/env python3
"""Run balanced native compiler comparisons with complete export coverage.

On hub: python3 scripts/dragline-iterate.py --cpu 7 --out /tmp/dragline-run
The default ten modules cover calls, integer work, FP, parsing, hashing, and
loop nests. They are a fixed iteration sample, not the final corpus gate.
"""
import argparse
import datetime
import hashlib
import json
import math
import os
from pathlib import Path
import platform
import re
import statistics
import subprocess

QUICK = "fib_rec,fannkuch,matmul,json-as,blake3,coremark,xxhash,kissfft,polybench-gemm,polybench-floyd-warshall"
ENGINES = {"railshot": "BenchmarkRailshotNativeExec", "dragline": "BenchmarkDraglineExec"}


def digest(path):
    return hashlib.sha256(path.read_bytes()).hexdigest()


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--out", type=Path, required=True)
    parser.add_argument("--cpu", type=int)
    parser.add_argument("--rounds", type=int, default=4)
    parser.add_argument("--benchtime", default="100ms")
    parser.add_argument("--pair-by-module", action="store_true", help="run the two engines consecutively for each module to limit drift")
    parser.add_argument("--corpus", default=QUICK, help="comma-separated IDs, a catalog profile, or all")
    parser.add_argument("--baseline-run", type=Path, help="compare Dragline with a saved run's Dragline executable instead of Railshot")
    args = parser.parse_args()
    if args.rounds < 2 or args.rounds % 2:
        parser.error("rounds must be even and at least two")
    root = Path(__file__).resolve().parents[1]
    out = args.out.resolve()
    out.mkdir(parents=True, exist_ok=False)
    env = dict(os.environ, GOMAXPROCS="1", GOWORK="off", GOFLAGS="-mod=readonly", WAGO_BOUNDS="signals")
    catalog_path = root / "corpus/catalog.json"
    catalog = json.loads(catalog_path.read_text())
    modules = {m["id"]: m for m in catalog["benchmarks"]}
    ids = list(modules) if args.corpus == "all" else catalog["profiles"].get(args.corpus, args.corpus.split(","))
    checks = {c["id"]: c for c in catalog["checks"]}
    expected = {}
    artifacts = {}
    for module_id in ids:
        module = modules[module_id]
        artifact = root / "corpus" / module["artifact"]
        artifacts[module_id] = digest(artifact)
        if artifacts[module_id] != module["artifact_sha256"]:
            raise RuntimeError(f"artifact hash mismatch: {module_id}")
        if module.get("stages") and "Exec" not in module["stages"]:
            continue
        exports = [e["export"] for e in module.get("exec", [])]
        exports += [checks[c]["invoke"]["export"] for c in module.get("semantic_exec", [])]
        for export in exports:
            name = f"{module_id}.{export}"
            if name in expected:
                raise RuntimeError(f"ambiguous duplicate export: {name}")
            expected[name] = module_id
    if not expected:
        raise RuntimeError("no runnable exports selected")
    source = hashlib.sha256()
    for path in sorted(root.rglob("*.go")):
        if ".git" in path.parts:
            continue
        source.update(str(path.relative_to(root)).encode() + b"\0" + path.read_bytes())
    binary = out / "suite.test"
    subprocess.run(["go", "test", "-tags", "wago_guardpage", "-c", "-o", str(binary), "./suite"],
                   cwd=root / "bench", env=env, check=True)
    prefix = ["taskset", "-c", str(args.cpu)] if args.cpu is not None else []
    metadata = {
        "utc": datetime.datetime.now(datetime.timezone.utc).isoformat(),
        "host": platform.node(), "platform": platform.platform(), "cpu": args.cpu,
        "go": subprocess.check_output(["go", "version"], text=True).strip(),
        "go_source_sha256": source.hexdigest(), "binary_sha256": digest(binary),
        "runner_sha256": digest(Path(__file__)),
        "catalog_sha256": digest(catalog_path), "artifact_sha256": artifacts,
        "corpus": ids, "expected_exports": sorted(expected),
        "rounds": args.rounds, "benchtime": args.benchtime, "pair_by_module": args.pair_by_module,
        "environment": {k: env[k] for k in ("GOMAXPROCS", "GOWORK", "GOFLAGS", "WAGO_BOUNDS")},
        "target": "native (both engines)",
    }
    engines = ENGINES
    binaries = {engine: binary for engine in engines}
    reference, candidate = "railshot", "dragline"
    metadata["comparison"] = "dragline_vs_railshot"
    if args.baseline_run:
        baseline_run = args.baseline_run.resolve()
        baseline = json.loads((baseline_run / "metadata.json").read_text())
        baseline_binary = baseline_run / "suite.test"
        if digest(baseline_binary) != baseline["binary_sha256"]:
            raise RuntimeError("baseline binary hash mismatch")
        for key in ("target", "environment", "catalog_sha256", "go", "platform", "host"):
            if baseline[key] != metadata[key]:
                raise RuntimeError(f"baseline metadata mismatch: {key}")
        for module_id, artifact_hash in artifacts.items():
            if baseline["artifact_sha256"].get(module_id) != artifact_hash:
                raise RuntimeError(f"baseline artifact mismatch: {module_id}")
        reference, candidate = "baseline", "candidate"
        engines = {reference: "BenchmarkDraglineExec", candidate: "BenchmarkDraglineExec"}
        binaries = {reference: baseline_binary, candidate: binary}
        metadata["comparison"] = "dragline_vs_baseline"
        metadata["baseline"] = {"run": str(baseline_run), "metadata": baseline}
        # Both binaries are measured now on args.cpu; the saved run's CPU
        # describes its historical samples, which are not reused here.
    (out / "metadata.json").write_text(json.dumps(metadata, indent=2) + "\n")
    samples = {engine: {name: [] for name in expected} for engine in engines}
    groups = [[module_id] for module_id in ids if module_id in expected.values()] if args.pair_by_module else [ids]
    for round_id in range(args.rounds):
        order = list(engines) if round_id % 2 == 0 else list(reversed(engines))
        rows_by_engine = {engine: {} for engine in engines}
        for group in groups:
            for engine in order:
                command = prefix + [str(binaries[engine]), "-test.run=^$", f"-test.bench=^{engines[engine]}$",
                                    f"-test.benchtime={args.benchtime}", "-test.count=1",
                                    "-wago.corpus=" + ",".join(group)]
                result = subprocess.run(command, cwd=root / "bench/suite", env=env,
                                        stdout=subprocess.PIPE, stderr=subprocess.STDOUT, text=True)
                with (out / f"round-{round_id+1}-{engine}.txt").open("a") as log:
                    log.write(result.stdout)
                if result.returncode:
                    raise RuntimeError(f"{engine} round {round_id+1} failed; see raw log")
                rows = rows_by_engine[engine]
                pattern = rf"^{engines[engine]}/(\S+?)(?:-\d+)?\s+\d+\s+([\d.]+) ns/op"
                for line in result.stdout.splitlines():
                    match = re.match(pattern, line)
                    if match:
                        name, value = match.groups()
                        if name in rows:
                            raise RuntimeError(f"duplicate row: {name}")
                        rows[name] = float(value)
            if args.pair_by_module:
                print(f"round {round_id+1}/{args.rounds} paired {group[0]}", flush=True)
        for engine, rows in rows_by_engine.items():
            if rows.keys() != expected.keys():
                raise RuntimeError(f"coverage mismatch: missing={expected.keys()-rows.keys()}, extra={rows.keys()-expected.keys()}")
            for name, value in rows.items():
                samples[engine][name].append(value)
            print(f"round {round_id+1}/{args.rounds} {engine}: {len(rows)} exports passed", flush=True)
    modules_ratios = {}
    exports = {}
    for name, module_id in expected.items():
        r, d = samples[reference][name], samples[candidate][name]
        ratio = statistics.median(b/a for a, b in zip(r, d))
        exports[name] = {f"{reference}_ns": statistics.median(r), f"{candidate}_ns": statistics.median(d), "paired_ratio": ratio}
        modules_ratios.setdefault(module_id, []).append(ratio)
    geomean = lambda values: math.exp(statistics.mean(math.log(x) for x in values))
    module_ratios = {m: geomean(values) for m, values in modules_ratios.items()}
    ratio = geomean(module_ratios.values())
    report = {"exports": exports, "module_ratios": module_ratios, "time_ratio": ratio,
              "time_reduction_percent": 100*(1-ratio), "throughput_gain_percent": 100*(1/ratio-1), "samples": samples,
              "sample_spread": {engine: {name: max(values)/min(values) for name, values in rows.items()}
                                for engine, rows in samples.items()}}
    (out / "results.json").write_text(json.dumps(report, indent=2) + "\n")
    unstable = [(engine, name, spread) for engine, rows in report["sample_spread"].items()
                for name, spread in rows.items() if spread > 1.20]
    if unstable:
        print(f"WARNING: {len(unstable)} engine/export samples vary by over 20%; inspect raw data before claiming a speedup")
    for name, value in exports.items():
        print(f"{name:55} {value['paired_ratio']:.4f}")
    print(f"Module-weighted paired time ratio: {ratio:.4f}; time reduction: {100*(1-ratio):.2f}%")


if __name__ == "__main__":
    main()
