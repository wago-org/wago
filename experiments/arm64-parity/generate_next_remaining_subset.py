"""Combine qualified focused captures without rerunning the full timing set."""
from decimal import Decimal
import hashlib
from pathlib import Path
from statistics import median

from generate_hour_remaining_subset import execution_pairs, historical_times

HERE = Path(__file__).resolve().parent
ROOT = HERE.parent.parent


def add_capture(rows, hashes, filename, names, rounds, protocol):
    raw = (HERE / filename).read_bytes()
    pairs = execution_pairs(raw)
    if set(pairs) != set(names):
        raise ValueError(f"{filename}: unexpected or missing workloads")
    hashes.append((filename, hashlib.sha256(raw).hexdigest()))
    for name, corpus in names.items():
        sample = pairs[name]
        if set(sample) != set(range(rounds)) or any(set(p) != {False, True} for p in sample.values()):
            raise ValueError(f"{filename}: incomplete or duplicate pairs for {name}")
        values = [sample[i][True] for i in range(rounds)]
        if corpus in rows:
            raise ValueError(f"duplicate current capture for {corpus}")
        rows[corpus] = (median(values), min(values), max(values), protocol)


def main():
    rows, hashes = {}, []
    old = "audio-adpcm video-dct files-glob-match compiler-register-allocation vision-components graphics-reed-solomon image-median document-edit-distance triangle-raster video-optical-flow compression-lzw geo-polygon-clipping stencil-wave-equation ml-knn language-register-vm vision-dilation".split()
    new = "search-aho-corasick stats-linear-regression ml-inference hardware-prime-implicants serialization-protobuf audio-fir grid-pathfinding ml-kmeans stats-bootstrap files-path-normalize hardware-boolean-anf ml-convolution".split()
    add_capture(rows, hashes, "hour-20261011-focused-exec.jsonl", {n: "applications/" + n for n in old}, 8, "37174169; 8×150ms")
    add_capture(rows, hashes, "next-20261011-gap-screen-exec.jsonl", {n: "applications/" + n for n in new}, 6, "37174169; 6×125ms")
    add_capture(rows, hashes, "hour-20261011-focused-core-exec.jsonl", {
        "modular-arithmetic": "wago/libtommath/modular-arithmetic",
        "decimal-parse": "wago/fastfloat/decimal-parse",
        "complex-roundtrip": "wago/kissfft/complex-roundtrip",
    }, 6, "37174169; 6×150ms")

    # Polygon is the only extra workload in the completed old/current process comparison.
    filename = "hour-20261011-cross-build.jsonl"
    raw = (HERE / filename).read_bytes()
    proof = HERE / "hour-20261011-cross-native-proof.txt"
    if not proof.exists() or proof.stat().st_mtime_ns < (HERE / filename).stat().st_mtime_ns:
        raise ValueError("cross-build lacks its final native proof marker")
    pairs = execution_pairs(raw)["geo-point-in-polygon"]
    if set(pairs) != set(range(4)) or any(set(p) != {False, True} for p in pairs.values()):
        raise ValueError("incomplete polygon process pairs")
    values = [pairs[i][True] for i in range(4)]
    rows["applications/geo-point-in-polygon"] = (median(values), min(values), max(values), "37174169; 4×100ms processes")
    hashes.append((filename, hashlib.sha256(raw).hexdigest()))

    historical = historical_times(ROOT / "steady-exec-wago-vs-w2c2.md")
    ranked = []
    for corpus, (current, low, high, protocol) in rows.items():
        if corpus not in historical:
            raise ValueError(f"missing successful historical comparator: {corpus}")
        recorded = historical[corpus]
        ranked.append((current / recorded, corpus, current, low, high, recorded, protocol))

    lines = ["# Current ARM64 priority subset, 2026-10-11", "",
        "Apple M4 Max, arm64. These 32 focused workloads combine completed captures of the qualified default compiler at `37174169`. No full timing sweep was performed. Times are median microseconds per invocation, after warmup, for each workload's exact cached contract. The latest exploratory changes are excluded until retained and qualified.", "",
        "The w2c2 column comes from the [complete historical device ranking](../../steady-exec-wago-vs-w2c2.md), captured 2026-10-09 for `w2c2-gcc` / `c-aot`, source `main@700f5565`. w2c2 was not rerun. Ratios combine different captures and protocols; they prioritize remaining work and do not prove current full-corpus parity. Positive gaps mean Wago is slower. Workloads map by exact corpus ID; missing timings are never zero.", "",
        "| Rank | Corpus workload | Current Wago µs | Current range µs | Historical w2c2 µs | Ratio | Gap | Current capture |",
        "|---:|---|---:|---:|---:|---:|---:|---|"]
    for rank, (ratio, corpus, current, low, high, recorded, protocol) in enumerate(sorted(ranked, reverse=True), 1):
        lines.append(f"| {rank} | `{corpus}` | {current:.6f} | {low:.6f}–{high:.6f} | {recorded:.6f} | {ratio:.3f}× | {(ratio-1)*100:+.1f}% | {protocol} |")
    lines += ["", "The hour application and core captures select all five retained optimization switches ON. The next 12-application capture selects `ordinary-shifted-immediate` ON, with the other retained features at their default. All ON rows represent the same qualified compiler defaults. The single polygon row uses the current-source process from four reversed old/current pairs. All remaining rows use alternating ON/OFF pairs in one frozen binary; only ON samples enter this ranking. These protocols request QoS33 and lock the OS thread, which does not establish physical-core affinity.", "",
        "The old 7.706× Aho–Corasick gap is now 1.304× in this subset. Linear regression and ML inference are approximately at historical w2c2 parity. Modular arithmetic, point-in-polygon, register allocation, glob matching, and the register VM remain higher priorities. This is a current priority list, not a claim that the missing 116 contracts have comparable fresh timings.", "",
        "Source SHA256:", ""]
    lines += [f"- `{name}`: `{digest}`" for name, digest in hashes]
    lines += ["", "See [the hour results](hour-20261011-results.md) for paired gains, compilation costs, exact validation, and native-image qualification. See [the new gap screen](next-20261011-gap-screen-summary.md) for the latest 12 switch comparisons; small movements in unchanged native images are not credited as execution improvements.", ""]
    output = HERE / "next-20261011-remaining-subset.md"
    output.write_text("\n".join(lines))
    print(f"Wrote {output}: {len(ranked)} ranked workloads")


if __name__ == "__main__":
    main()
