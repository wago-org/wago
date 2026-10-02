#!/usr/bin/env python3
from __future__ import annotations

import argparse
import math
import statistics
from pathlib import Path

from analyze_optimization_matrix import parse_raw


def geomean(values: list[float]) -> float:
    return math.exp(sum(math.log(value) for value in values) / len(values))


def captures(root: Path, profile: str) -> list[dict[str, dict[str, float]]]:
    return [parse_raw(path) for path in sorted((root / "raw").glob(f"{profile}_r*.txt"))]


def main() -> None:
    parser = argparse.ArgumentParser()
    parser.add_argument("roots", nargs="+", help="ARCH=RESULT_DIRECTORY")
    args = parser.parse_args()

    for item in args.roots:
        arch, root_text = item.split("=", 1)
        root = Path(root_text)
        old = captures(root, "old-default")
        new = captures(root, "new-default")
        if len(old) != 4 or len(new) != 4:
            raise SystemExit(f"{arch}: expected four captures per profile")
        rows = set(old[0])
        if any(set(sample) != rows for sample in old + new):
            raise SystemExit(f"{arch}: benchmark rows differ")

        print(arch)
        for stage, metric in (("exec", "time"), ("compile", "time"), ("compile", "bytes"), ("compile", "allocs")):
            ratios: list[float] = []
            deltas: list[tuple[float, str]] = []
            for row in sorted(row for row in rows if row.startswith(stage + "/")):
                old_median = statistics.median(sample[row][metric] for sample in old)
                new_median = statistics.median(sample[row][metric] for sample in new)
                ratio = new_median / old_median
                ratios.append(ratio)
                deltas.append(((ratio - 1) * 100, row.removeprefix(stage + "/")))
            delta = (geomean(ratios) - 1) * 100
            worst = max(deltas)
            best = min(deltas)
            print(f"  {stage}_{metric}: {delta:+.2f}%  worst={worst[1]} {worst[0]:+.2f}%  best={best[1]} {best[0]:+.2f}%")


if __name__ == "__main__":
    main()
