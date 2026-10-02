#!/usr/bin/env python3
from __future__ import annotations

import argparse
import math
import re
import statistics
from dataclasses import dataclass
from pathlib import Path


BENCH_RE = re.compile(
    r"^(BenchmarkMatrix(?:CompileFull|Exec))/([^\s]+)\s+\d+\s+"
    r"([0-9.]+)\s+ns/op\s+([0-9.]+)\s+B/op\s+([0-9.]+)\s+allocs/op$"
)
FILE_RE = re.compile(r"^(.*)_(true|false)_r([1-4])\.txt$")


@dataclass(frozen=True)
class Opt:
    name: str
    selected: bool
    experimental: bool
    label: str
    desc: str


@dataclass
class Result:
    valid: bool
    reason: str
    rows: dict[str, dict[str, dict[str, float]]]
    aggregate: dict[str, dict[str, float | str]]


def geomean(values: list[float]) -> float:
    positive = [value for value in values if value > 0]
    return math.exp(sum(math.log(value) for value in positive) / len(positive)) if positive else 0.0


def median(values: list[float]) -> float:
    return statistics.median(values)


def inventory(path: Path) -> list[Opt]:
    opts: list[Opt] = []
    for line in path.read_text().splitlines():
        if not line.startswith("MATRIX_OPT\t"):
            continue
        _, name, selected, experimental, label, desc = line.split("\t", 5)
        opts.append(Opt(name, selected == "true", experimental == "true", label, desc))
    return opts


def parse_raw(path: Path) -> dict[str, dict[str, float]]:
    result: dict[str, dict[str, float]] = {}
    for line in path.read_text(errors="replace").splitlines():
        match = BENCH_RE.match(line)
        if not match:
            continue
        stage = "compile" if match.group(1).endswith("CompileFull") else "exec"
        result[f"{stage}/{match.group(2)}"] = {
            "time": float(match.group(3)),
            "bytes": float(match.group(4)),
            "allocs": float(match.group(5)),
        }
    return result


def analyze(root: Path, opts: list[Opt]) -> dict[str, Result]:
    captures: dict[str, dict[bool, list[dict[str, dict[str, float]]]]] = {
        opt.name: {True: [], False: []} for opt in opts
    }
    exits: dict[str, dict[bool, list[int]]] = {opt.name: {True: [], False: []} for opt in opts}
    for path in sorted((root / "raw").glob("*.txt")):
        match = FILE_RE.match(path.name)
        if not match:
            continue
        name, state_text, _ = match.groups()
        state = state_text == "true"
        captures[name][state].append(parse_raw(path))
        exits[name][state].append(int(path.with_suffix(path.suffix + ".exit").read_text().strip()))

    results: dict[str, Result] = {}
    for opt in opts:
        failures = sum(status != 0 for state in exits[opt.name].values() for status in state)
        counts = {state: len(captures[opt.name][state]) for state in (True, False)}
        if failures or counts != {True: 4, False: 4}:
            results[opt.name] = Result(
                False,
                f"{failures} nonzero exits; samples on/off={counts[True]}/{counts[False]}",
                {},
                {},
            )
            continue
        row_sets = {
            state: [set(sample) for sample in captures[opt.name][state]] for state in (True, False)
        }
        expected = row_sets[True][0]
        if any(rows != expected for state in (True, False) for rows in row_sets[state]):
            results[opt.name] = Result(False, "benchmark row sets differ across samples", {}, {})
            continue

        rows: dict[str, dict[str, dict[str, float]]] = {}
        for row in sorted(expected):
            rows[row] = {}
            for metric in ("time", "bytes", "allocs"):
                on_values = [sample[row][metric] for sample in captures[opt.name][True]]
                off_values = [sample[row][metric] for sample in captures[opt.name][False]]
                on = median(on_values)
                off = median(off_values)
                delta = (off / on - 1.0) * 100.0 if on else 0.0
                on_spread = (max(on_values) / min(on_values) - 1.0) * 100.0 if min(on_values) else 0.0
                off_spread = (max(off_values) / min(off_values) - 1.0) * 100.0 if min(off_values) else 0.0
                rows[row][metric] = {
                    "on": on,
                    "off": off,
                    "delta": delta,
                    "spread": max(on_spread, off_spread),
                }

        aggregate: dict[str, dict[str, float | str]] = {}
        for stage, metric, key in (
            ("exec", "time", "exec_time"),
            ("compile", "time", "compile_time"),
            ("compile", "bytes", "compile_bytes"),
            ("compile", "allocs", "compile_allocs"),
        ):
            selected = [(row, values[metric]) for row, values in rows.items() if row.startswith(stage + "/")]
            ratios = [values["off"] / values["on"] for _, values in selected if values["on"] > 0]
            deltas = [(row, values["delta"]) for row, values in selected]
            spreads = [values["spread"] for _, values in selected]
            aggregate[key] = {
                "rows": float(len(selected)),
                "on": geomean([values["on"] for _, values in selected]),
                "off": geomean([values["off"] for _, values in selected]),
                "delta": (geomean(ratios) - 1.0) * 100.0,
                "median_spread": median(spreads),
                "worst_row": max(deltas, key=lambda item: item[1])[0] if deltas else "",
                "worst": max((value for _, value in deltas), default=0.0),
                "best_row": min(deltas, key=lambda item: item[1])[0] if deltas else "",
                "best": min((value for _, value in deltas), default=0.0),
            }
        results[opt.name] = Result(True, "", rows, aggregate)
    return results


def pct(value: float) -> str:
    return f"{value:+.2f}%"


def number(value: float, metric: str) -> str:
    if metric == "time":
        if value >= 1_000_000:
            return f"{value / 1_000_000:.3f} ms"
        if value >= 1_000:
            return f"{value / 1_000:.3f} us"
        return f"{value:.2f} ns"
    if metric == "bytes":
        if value >= 1024 * 1024:
            return f"{value / (1024 * 1024):.2f} MiB"
        if value >= 1024:
            return f"{value / 1024:.2f} KiB"
        return f"{value:.0f} B"
    return f"{value:.1f}"


def classification(result: Result) -> str:
    if not result.valid:
        return "failed"
    exec_delta = float(result.aggregate["exec_time"]["delta"])
    worst = float(result.aggregate["exec_time"]["worst"])
    compile_delta = float(result.aggregate["compile_time"]["delta"])
    bytes_delta = float(result.aggregate["compile_bytes"]["delta"])
    if exec_delta >= 1.0 or worst >= 5.0:
        return "retain"
    if abs(exec_delta) <= 0.25 and worst <= 2.0 and compile_delta <= 0.50 and bytes_delta <= 0.10:
        return "removal-screen"
    return "mixed/noisy"


def metadata(path: Path) -> dict[str, str]:
    result: dict[str, str] = {}
    for line in path.read_text().splitlines():
        if "=" in line:
            key, value = line.split("=", 1)
            result[key] = value
    return result


def report(roots: dict[str, Path], output: Path) -> None:
    inventories = {arch: inventory(root / "inventory.txt") for arch, root in roots.items()}
    analyses = {arch: analyze(root, inventories[arch]) for arch, root in roots.items()}
    metas = {arch: metadata(root / "metadata.txt") for arch, root in roots.items()}

    lines: list[str] = []
    add = lines.append
    add("# Wago optimization toggle matrix: ARM64 and AMD64")
    add("")
    add("> Generated from raw benchmark captures on 2026-08-22. Each catalog optimization was measured explicitly on and off while every other option remained at its host-selected default.")
    add("")
    add("## Executive summary")
    add("")
    add(f"- Coverage: {len(inventories['arm64'])} ARM64 options and {len(inventories['amd64'])} AMD64 options, four samples per state.")
    failures = [(arch, opt.name, analyses[arch][opt.name].reason) for arch in roots for opt in inventories[arch] if not analyses[arch][opt.name].valid]
    if failures:
        add("- Fail-closed result: " + "; ".join(f"`{arch}/{name}` ({reason})" for arch, name, reason in failures) + ".")
    add("- Percentages below are **disabled versus enabled**. Positive execution time means disabling made execution slower (the optimization helped); negative means disabling made execution faster.")
    add("- This is a broad screening matrix, not automatic deletion authority. Correctness/safety responsibilities, native-code size, static hit counts, and focused reruns still gate removal.")
    add("")
    add("## Environment")
    add("")
    add("| Architecture | Commit | CPU / OS | Go | GOMAXPROCS | Affinity | Benchtime | Samples/state |")
    add("|---|---|---|---|---:|---|---:|---:|")
    for arch in ("arm64", "amd64"):
        meta = metas[arch]
        cpu = meta.get("uname", "").replace("|", "\\|")
        add(f"| {arch} | `{meta.get('commit', '')[:12]}` | {cpu} | `{meta.get('go_version', '')}` | {meta.get('gomaxprocs', '')} | {meta.get('cpu_affinity', '')} | {meta.get('benchtime', '')} | {meta.get('samples_per_state', '')} |")
    add("")
    add("## Method")
    add("")
    add("1. The source was detached at exact `origin/main` commit `ef129fdbb8201048077eeea484637bd4a628d4cc` in isolated local and remote worktrees.")
    add("2. The canonical architecture catalog (`OptimizationInfos`) supplied the inventory; unregistered legacy environment switches were intentionally excluded.")
    add("3. Each process selected one option with immutable `RuntimeConfig.WithOptimization(name, state)`. All other options retained host defaults; compilation used one function worker.")
    add("4. Each option ran in ABBA-balanced state order over four samples per state. `BenchmarkMatrixCompileFull` measured decode + validate + codegen and reported `ns/op`, `B/op`, and `allocs/op`; `BenchmarkMatrixExec` measured prepared host-to-Wasm calls.")
    add("5. Per-workload values are medians of four process samples. Aggregate deltas are geometric means of per-workload off/on ratios, preventing Ruby/esbuild compile latency from numerically drowning small modules.")
    add("6. ARM64 used Apple M4 Max with `GOMAXPROCS=1`; AMD64 used Ryzen 7 7800X3D with `GOMAXPROCS=1` and `taskset -c 0`.")
    add("")
    add("## Reading the tables")
    add("")
    add("- `Exec delta`: geometric-mean execution-time change when disabled.")
    add("- `Compile delta`: geometric-mean full-compile-time change when disabled.")
    add("- `Compile B delta`: geometric-mean bytes allocated per full compile when disabled.")
    add("- `Worst exec row`: largest workload slowdown when disabled; it protects optimizations with narrow but material wins from being hidden by a neutral aggregate.")
    add("- `Spread`: median within-state max/min spread across the four samples; effects near or below spread should be treated as noise.")
    add("- `removal-screen` is deliberately narrow: |exec| <= 0.25%, worst slowdown <= 2%, compile <= +0.5%, compile bytes <= +0.1%. It is only a shortlist.")
    add("")

    for arch in ("arm64", "amd64"):
        add(f"## {arch.upper()} complete catalog")
        add("")
        add("| Optimization | Selected default | Experimental | Exec delta | Compile delta | Compile B delta | Worst exec row | Exec spread | Triage |")
        add("|---|:---:|:---:|---:|---:|---:|---|---:|---|")
        for opt in inventories[arch]:
            result = analyses[arch][opt.name]
            if not result.valid:
                add(f"| `{opt.name}` | {'on' if opt.selected else 'off'} | {'yes' if opt.experimental else 'no'} | failed | failed | partial | {result.reason} | n/a | **failed** |")
                continue
            exec_agg = result.aggregate["exec_time"]
            add(
                f"| `{opt.name}` | {'on' if opt.selected else 'off'} | {'yes' if opt.experimental else 'no'} | "
                f"{pct(float(exec_agg['delta']))} | {pct(float(result.aggregate['compile_time']['delta']))} | "
                f"{pct(float(result.aggregate['compile_bytes']['delta']))} | "
                f"`{str(exec_agg['worst_row']).removeprefix('exec/')}` {pct(float(exec_agg['worst']))} | "
                f"{float(exec_agg['median_spread']):.2f}% | {classification(result)} |"
            )
        add("")

    add("## Mechanical removal screen")
    add("")
    add("These pass the numeric screen only. The interpretation section below must still exclude safety mechanisms, compatibility paths, size-only optimizations, and options with known focused workloads outside this corpus.")
    add("")
    add("| Architecture | Optimization | Exec delta | Worst exec slowdown | Compile delta | Compile B delta |")
    add("|---|---|---:|---:|---:|---:|")
    for arch in ("arm64", "amd64"):
        for opt in inventories[arch]:
            result = analyses[arch][opt.name]
            if classification(result) != "removal-screen":
                continue
            add(
                f"| {arch} | `{opt.name}` | {pct(float(result.aggregate['exec_time']['delta']))} | "
                f"{pct(float(result.aggregate['exec_time']['worst']))} | {pct(float(result.aggregate['compile_time']['delta']))} | "
                f"{pct(float(result.aggregate['compile_bytes']['delta']))} |"
            )
    add("")

    add("## Detailed per-option results")
    add("")
    add("Each subsection lists aggregate on/off medians plus the five execution rows most helped and most hurt by disabling. Workload deltas use the same off-versus-on sign convention.")
    add("")
    for arch in ("arm64", "amd64"):
        add(f"### {arch.upper()}")
        add("")
        for opt in inventories[arch]:
            result = analyses[arch][opt.name]
            add(f"#### `{opt.name}` — {opt.label}")
            add("")
            add(f"{opt.desc.capitalize()}. Selected default: **{'on' if opt.selected else 'off'}**; experimental: **{'yes' if opt.experimental else 'no'}**; triage: **{classification(result)}**.")
            add("")
            if not result.valid:
                add(f"Measurement failed: {result.reason}. Partial output is retained in the raw capture directory and excluded from aggregates.")
                add("")
                continue
            add("| Metric | Enabled geomean | Disabled geomean | Disabled delta | Median sample spread |")
            add("|---|---:|---:|---:|---:|")
            for key, label, metric in (
                ("exec_time", "Execution time", "time"),
                ("compile_time", "Full compile time", "time"),
                ("compile_bytes", "Full compile bytes", "bytes"),
                ("compile_allocs", "Full compile allocations", "allocs"),
            ):
                agg = result.aggregate[key]
                add(f"| {label} | {number(float(agg['on']), metric)} | {number(float(agg['off']), metric)} | {pct(float(agg['delta']))} | {float(agg['median_spread']):.2f}% |")
            add("")
            exec_rows = [(row.removeprefix("exec/"), values["time"]["delta"]) for row, values in result.rows.items() if row.startswith("exec/")]
            exec_rows.sort(key=lambda item: item[1], reverse=True)
            notable = exec_rows[:5] + list(reversed(exec_rows[-5:]))
            add("| Execution workload | Disabled delta | Direction |")
            add("|---|---:|---|")
            seen: set[str] = set()
            for row, delta in notable:
                if row in seen:
                    continue
                seen.add(row)
                direction = "optimization helps" if delta > 0 else "disabled is faster"
                add(f"| `{row}` | {pct(delta)} | {direction} |")
            add("")

    add("## Reproduction")
    add("")
    add("The benchmark-only harness and runner are uncommitted files in this isolated worktree:")
    add("")
    add("- `bench/optimization_matrix_bench_test.go`")
    add("- `bench/run_optimization_matrix.sh`")
    add("- `bench/results/arm64/` and `bench/results/amd64/` raw captures")
    add("")
    add("Representative invocation:")
    add("")
    add("```sh")
    add("cd bench")
    add("go test -c -o optimization-matrix.test .")
    add("./run_optimization_matrix.sh results/arm64")
    add("MATRIX_CPU=0 ./run_optimization_matrix.sh results/amd64")
    add("```")
    add("")
    add("## Limitations and next gates")
    add("")
    add("- Four 100 ms samples are suitable for broad screening, not sub-percent release claims. Any deletion candidate needs longer focused interleaved reruns.")
    add("- The matrix measures compiler heap allocation (`B/op`), not peak RSS. Peak process RSS requires a separate one-shot harness and should be added before removing a mechanism primarily justified by bounded scratch reuse.")
    add("- Native-code size is not part of this request's three metrics. Size-objective and code-size-only transformations can look neutral here and must not be deleted without code-byte measurements.")
    add("- The executable corpus cannot run Ruby, esbuild, SQLite, Lua, wasm3, or regexmatch because their host environments are absent. They still contribute full compile time and memory.")
    add("- This matrix toggles registered optimizations individually. It does not test interactions among multiple disabled options or undocumented legacy environment switches.")
    add("- A crash or failure with an option disabled is a compatibility/correctness finding, not evidence that the optimization is fast.")
    add("")
    output.write_text("\n".join(lines) + "\n")


def main() -> None:
    parser = argparse.ArgumentParser()
    parser.add_argument("--arm64", type=Path, required=True)
    parser.add_argument("--amd64", type=Path, required=True)
    parser.add_argument("--output", type=Path, required=True)
    args = parser.parse_args()
    report({"arm64": args.arm64, "amd64": args.amd64}, args.output)


if __name__ == "__main__":
    main()
