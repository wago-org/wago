# Integrated direct16 validation

Code source: `93c7ec34`; measurement checkout/documentation source: `4d524db8cbbcb007ca9d80f2c9785d755bca5087`. The latter adds validation documentation only. Linux x86-64 / Intel Xeon Platinum 8573C, Go 1.27.1, Node 24.22.1 / V8 13.6.233.17-node.58 with `--no-liftoff`. Exact binaries, build tags, artifact hashes, configuration and CPU are retained in raw evidence.

## Protocol and completeness

Audit 18/18 receipts and full gate 72/72 receipts completed successfully, none missing. All checksums/artifact hashes match the manifest. Wago native direct views installed; allocation elision and compact/age2 disabled. Every audit case/variant reports allocations and actual collections after 128 warmups. Runtime gate: 100 separate warmups, nine batches of 32 calls, three rotating fresh-process rounds; GOMAXPROCS=1. 2 MiB young / 2 × 8 MiB old. Serialized under a coordinated CPU reservation. The six local synthetic artifacts are not a completed wasm.fyi suite; results are end-to-end execution, not isolated collector cost. V8 internal allocation elimination remains unmeasured.

## Runtime

Medians across 27 per-call batch samples per case/variant (µs). Ratios are Wago/V8; ≤1.15× is the per-case target. No aggregate ratio hides case misses.

| Case | Baseline µs | Paired µs | Range µs | V8 µs | Baseline/V8 | Paired/V8 | Range/V8 | Target verdict |
|---|---:|---:|---:|---:|---:|---:|---:|---|
| struct-allocation-access | 5734.204 | 5842.136 | 5748.232 | 1404.113 | 4.084× | 4.161× | 4.094× | baseline miss, paired miss, range miss |
| array-allocation-access | 15444.474 | 12381.960 | 12308.699 | 7878.647 | 1.960× | 1.572× | 1.562× | baseline miss, paired miss, range miss |
| array-fill | 14685.215 | 14845.973 | 14238.156 | 10339.110 | 1.420× | 1.436× | 1.377× | baseline miss, paired miss, range miss |
| array-copy-checksum | 285.520 | 387.603 | 329.642 | 165.899 | 1.721× | 2.336× | 1.987× | baseline miss, paired miss, range miss |
| array-loop-copy-checksum | 500.217 | 527.074 | 243.952 | 181.370 | 2.758× | 2.906× | 1.345× | baseline miss, paired miss, range miss |
| tree-survival | 2001.026 | 1930.523 | 2128.039 | 1045.906 | 1.913× | 1.846× | 2.035× | baseline miss, paired miss, range miss |

### Same-source candidate attribution

| Case | Paired/baseline | Range/baseline |
|---|---:|---:|
| struct-allocation-access | 1.019× | 1.002× |
| array-allocation-access | 0.802× | 0.797× |
| array-fill | 1.011× | 0.970× |
| array-copy-checksum | 1.358× | 1.155× |
| array-loop-copy-checksum | 1.054× | 0.488× |
| tree-survival | 0.965× | 1.063× |

These comparisons change compiler-stage options, not just the collector. Control movement/regressions are retained; repeatability across hosts and workloads is not established. Passing a single case is not overall V8 parity.

## Compilation footprint

Three rotating fresh-process rounds per variant, 256 Compile+Close iterations per case, GC_COMPILE_PROBE=1/GOMAXPROCS=1. Artifact digests validated by the benchmark. Median ns/op, bytes/op, allocations/op and emitted code bytes reported below. Go allocation bytes are not peak compilation RSS. Input loading/configuration is outside timing.

| Case | Variant | ns/op | B/op | allocs/op | code-B |
|---|---|---:|---:|---:|---:|
| struct-allocation-access | baseline | 176433 | 56026 | 232 | 1775 |
| array-allocation-access | baseline | 240157 | 56075 | 223 | 1967 |
| array-fill | baseline | 251973 | 59979 | 233 | 2543 |
| array-copy-checksum | baseline | 344168 | 76508 | 276 | 4719 |
| array-loop-copy-checksum | baseline | 356094 | 76508 | 276 | 4719 |
| tree-survival | baseline | 223862 | 57339 | 267 | 1455 |
| struct-allocation-access | paired | 208848 | 56026 | 232 | 1775 |
| array-allocation-access | paired | 174087 | 56075 | 223 | 1967 |
| array-fill | paired | 226830 | 59979 | 233 | 2543 |
| array-copy-checksum | paired | 312102 | 76508 | 276 | 4719 |
| array-loop-copy-checksum | paired | 278776 | 76508 | 276 | 4719 |
| tree-survival | paired | 188247 | 57339 | 267 | 1455 |
| struct-allocation-access | range | 207680 | 56026 | 232 | 1775 |
| array-allocation-access | range | 170636 | 56075 | 223 | 1967 |
| array-fill | range | 229394 | 59979 | 233 | 2543 |
| array-copy-checksum | range | 282472 | 76508 | 276 | 4783 |
| array-loop-copy-checksum | range | 259113 | 76508 | 276 | 4783 |
| tree-survival | range | 194105 | 57339 | 267 | 1455 |

All 54 case/variant/round compilation measurements completed. Runtime RSS receipts and binary sizes remain in raw JSON; they are not attributed peak collector/compilation costs. No claim of passing all GC tests is made by this benchmark report; consult the separate qualification evidence.

## Verdict

**All six cases miss the ≤1.15× V8 target in all three variants.** Range is promising for scalar loop-copy (0.488× same-source baseline), but array-copy regresses to 1.155× baseline and tree-survival to 1.063× baseline. Paired array-copy regresses to 1.358× baseline. These are not grounds for enabling either candidate globally or claiming V8 parity.

Compilation allocation bytes/counts are identical across the three variants in each case. Range adds 64 emitted bytes to each copy case (4719 → 4783); other measured code sizes are unchanged. Compilation timing variation includes control cases with identical code/allocation footprints, so do not attribute all timing movement to the candidate. Peak compilation memory and cross-host reproducibility remain unmeasured.
