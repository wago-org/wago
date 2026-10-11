"""Rank the final application ON medians against recorded, unpaired w2c2."""
import argparse
from collections import defaultdict
from datetime import datetime, timezone
from decimal import Decimal, InvalidOperation
import hashlib
from pathlib import Path
import re
from statistics import median, quantiles
import json

HERE = Path(__file__).resolve().parent
ROOT = HERE.parent.parent


def historical_times(path):
    values = {}
    for line in path.read_text().splitlines():
        cells = [cell.strip() for cell in line.strip().strip("|").split("|")]
        if len(cells) != 6 or not cells[0].isdigit():
            continue
        name = cells[1].strip("`")
        try:
            value = Decimal(cells[3])
        except InvalidOperation:
            continue
        if value.is_finite() and value > 0:
            if name in values:
                raise ValueError(f"duplicate historical corpus: {name}")
            values[name] = value
    return values


def execution_pairs(raw):
    pairs = defaultdict(lambda: defaultdict(dict))
    for line in raw.decode().splitlines():
        if not line.startswith("{"):
            continue  # harness/QoS diagnostics are not measurements
        row = json.loads(line, parse_float=Decimal)
        if row["phase"] != "exec":
            continue
        value = Decimal(str(row["us"]))
        if type(row["on"]) is not bool or type(row["round"]) is not int or row["round"] < 0 or not value.is_finite() or value <= 0:
            raise ValueError(f"invalid execution sample: {row}")
        pair = pairs[row["workload"]][row["round"]]
        if row["on"] in pair:
            raise ValueError(f"duplicate sample: {row['workload']} round {row['round']}")
        pair[row["on"]] = value
    return pairs


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--input", type=Path, default=HERE / "hour-20261011-focused-exec.jsonl")
    parser.add_argument("--output", type=Path, default=HERE / "hour-20261011-remaining-subset.md")
    parser.add_argument("--historical", type=Path, default=ROOT / "steady-exec-wago-vs-w2c2.md")
    parser.add_argument("--plan", type=Path, default=HERE / "hour-20261011-focused.sh")
    parser.add_argument("--cross-build", type=Path, default=HERE / "hour-20261011-cross-build.jsonl")
    parser.add_argument("--prior", type=Path, default=HERE / "remaining-retained-current-subset.md")
    args = parser.parse_args()
    plan = args.plan.read_text()
    names = re.search(r"^workloads=(\S+)$", plan, re.MULTILINE).group(1).split(",")
    options = re.search(r"^options=(\S+)$", plan, re.MULTILINE).group(1)
    expected = int(re.search(r"-rounds\s+(\d+)", plan).group(1))
    budget = re.search(r"-budget\s+(\S+)", plan).group(1)
    if len(set(names)) != len(names):
        raise ValueError("duplicate workload in focused plan")
    historical = historical_times(args.historical)
    # Cross-check overlapping comparators in the earlier retained-subset report.
    if args.prior.exists():
        for line in args.prior.read_text().splitlines():
            cells = [cell.strip() for cell in line.strip().strip("|").split("|")]
            if len(cells) == 6 and cells[0] in names + ["geo-point-in-polygon"]:
                corpus = "applications/" + cells[0]
                if corpus in historical and Decimal(cells[3]) != historical[corpus]:
                    raise ValueError(f"historical w2c2 comparator conflicts with prior report: {corpus}")
    raw = args.input.read_bytes()
    pairs = execution_pairs(raw)
    if not pairs:
        raise ValueError("input contains no execution samples; run after focused timings complete")
    ranked, paired, skipped = [], [], []
    for name in names:
        corpus = "applications/" + name
        rounds = pairs.get(name, {})
        if set(rounds) != set(range(expected)) or any(set(pair) != {False, True} for pair in rounds.values()):
            skipped.append((corpus, f"expected {expected} complete ON/OFF rounds; found {len(rounds)} rounds ({sum(len(p) == 2 for p in rounds.values())} complete)"))
            continue
        off, on = ([rounds[i][state] for i in range(expected)] for state in (False, True))
        changes = [(rounds[i][True] / rounds[i][False] - 1) * 100 for i in range(expected)]
        quartiles = quantiles(changes, n=4, method="inclusive")
        current = median(on)
        paired.append((median(changes), corpus, median(off), current, quartiles[0], quartiles[2]))
        if corpus not in historical:
            skipped.append((corpus, "no successful numeric w2c2 steady timing for this exact corpus ID in the historical ranking"))
            continue
        recorded = historical[corpus]
        ranked.append((current / recorded, corpus, current, min(on), max(on), recorded, f"{expected}×{budget} same-thread ON"))
    for name in sorted(set(pairs) - set(names)):
        skipped.append((name, "execution samples are outside the planned application subset"))
    cross_paired, cross_raw = [], None
    cross_names = ["audio-adpcm", "video-dct", "graphics-reed-solomon", "files-glob-match", "image-median", "compiler-register-allocation", "vision-components", "geo-point-in-polygon"]
    cross_corpus = "applications/geo-point-in-polygon"
    cross_reason = "optional cross-build batch is not complete; no candidate time substituted"
    if args.cross_build.exists():
        candidate_raw = args.cross_build.read_bytes()
        try:
            cross = execution_pairs(candidate_raw)
            # The producer writes the proof only after both timing phases finish.
            proof = args.cross_build.with_name("hour-20261011-cross-native-proof.txt")
            complete = proof.exists() and proof.stat().st_mtime_ns >= args.cross_build.stat().st_mtime_ns and all(set(cross.get(name, {})) == set(range(4)) and all(set(pair) == {False, True} for pair in cross[name].values()) for name in cross_names)
            if complete:
                cross_raw = candidate_raw
                for name in cross_names:
                    rounds = cross[name]
                    off, on = ([rounds[i][state] for i in range(4)] for state in (False, True))
                    changes = [(rounds[i][True] / rounds[i][False] - 1) * 100 for i in range(4)]
                    quartiles = quantiles(changes, n=4, method="inclusive")
                    corpus = "applications/" + name
                    cross_paired.append((median(changes), corpus, median(off), median(on), quartiles[0], quartiles[2]))
                    if name == "geo-point-in-polygon":
                        if corpus in historical:
                            current, recorded = median(on), historical[corpus]
                            ranked.append((current / recorded, corpus, current, min(on), max(on), recorded, "4×100ms reversed process"))
                        else:
                            cross_reason = "no successful numeric historical w2c2 time for this exact corpus ID"
            else:
                cross_reason = "optional cross-build batch lacks four complete reversed-process pairs or its final native-proof marker"
        except (json.JSONDecodeError, ValueError, KeyError):
            cross_reason = "optional cross-build JSONL is incomplete or invalid; no candidate time substituted"
    if not any(row[1] == cross_corpus for row in ranked):
        skipped.append((cross_corpus, cross_reason))
    lines = ["# Remaining ARM64 gaps after the 2026-10-11 focused batch", "",
        f"Apple M4 Max, arm64. Fresh current Wago times are ON medians from {expected} alternating {budget} rounds in the frozen `/tmp/paired-hour-20261011` build. Warmups are excluded; times are microseconds per invocation. The first {len(names)} applications use that same-thread protocol; point-in-polygon is added only when its separate cross-build batch is complete. It uses four reversed-process 100ms pairs and the current candidate/ON median. `geo-point-in-polygon` and `geo-polygon-clipping` are distinct workloads.", "",
        "The w2c2 values are the exact steady medians reported in [the historical device ranking](../../steady-exec-wago-vs-w2c2.md), captured 2026-10-09 for `w2c2-gcc` / `c-aot`, source `main@700f5565`. Workloads map by exact `applications/<name>` corpus ID. w2c2 was not rerun. These ratios are unpaired across captures and protocols and serve subset prioritization; they do not establish current w2c2 performance or full-corpus parity.", "",
        "## Fresh current versus historical w2c2", "",
        "Worst-to-best is descending current Wago / historical w2c2. Positive gap means Wago is slower.", "",
        "| Rank | Corpus workload | Fresh ON median µs | ON range µs | Historical w2c2 steady µs | Ratio | Gap | Current source |",
        "|---:|---|---:|---:|---:|---:|---:|---|"]
    for rank, (ratio, corpus, current, low, high, recorded, protocol) in enumerate(sorted(ranked, reverse=True), 1):
        lines.append(f"| {rank} | `{corpus}` | {current:.6f} | {low:.6f}–{high:.6f} | {recorded:.6f} | {ratio:.3f}× | {(ratio-1)*100:+.1f}% | {protocol} |")
    lines += ["", "## Paired hour changes", "",
        f"OFF disables, and ON enables, these {len(options.split(','))} options in the same frozen binary: `{options}`. This measures their combined switches, not a complete revision-to-revision comparison. Median per-round ratios remain separate from the historical comparator above. Negative change means faster. Rows are ordered from largest slowdown to largest speedup.", "",
        "| Corpus workload | OFF median µs | ON median µs | Median paired change | Paired change IQR |",
        "|---|---:|---:|---:|---:|"]
    for change, corpus, off, on, low, high in sorted(paired, reverse=True):
        lines.append(f"| `{corpus}` | {off:.6f} | {on:.6f} | {change:+.2f}% | {low:+.2f}% to {high:+.2f}% |")
    if cross_paired:
        lines += ["", "## Separate source-build comparisons", "",
            "Four reversed-process 100ms pairs compare the actual pristine hour-start compiler (`c74d21c1`, `/tmp/paired-hour-base-c74d`) with the current default-on compiler (`/tmp/paired-hour-20261011`). Each process supplies its ordinary loop-memory-base ON state. These source-build changes use separate processes; they are not the 16 same-thread switch comparisons above. The ranking retains focused ON medians for those 16 and adds only point-in-polygon from this process protocol.", "",
            "| Corpus workload | Old compiler median µs | Current candidate median µs | Median process-paired change | Paired change IQR |",
            "|---|---:|---:|---:|---:|"]
        for change, corpus, off, on, low, high in sorted(cross_paired, reverse=True):
            lines.append(f"| `{corpus}` | {off:.6f} | {on:.6f} | {change:+.2f}% | {low:+.2f}% to {high:+.2f}% |")
    if skipped:
        lines += ["", "## Skipped or unranked", "", "| Workload | Reason |", "|---|---|"]
        lines += [f"| `{corpus}` | {reason} |" for corpus, reason in skipped]
    lines += ["", f"Ranked {len(ranked)}/{len(names)+1} applications ({len(names)} focused plus point-in-polygon); {len(paired)} complete same-thread cases and {len(cross_paired)} complete source-build cases. No missing value is treated as zero.", "",
        f"Generated {datetime.now(timezone.utc).isoformat(timespec='seconds')}. Raw input: `{args.input.name}`, SHA256 `{hashlib.sha256(raw).hexdigest()}`. The original historical ranking is unchanged. OS-thread locking and requested QoS33 do not establish physical-core affinity.", ""]
    if cross_raw is not None:
        lines += [f"Cross-build raw input: `{args.cross_build.name}`, SHA256 `{hashlib.sha256(cross_raw).hexdigest()}`.", ""]
    args.output.write_text("\n".join(lines))
    print(f"Wrote {args.output}: {len(ranked)} ranked, {len(paired)} paired, {len(skipped)} skipped/unranked")


if __name__ == "__main__":
    main()
