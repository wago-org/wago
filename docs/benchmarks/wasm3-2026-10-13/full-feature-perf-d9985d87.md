# Full feature performance suite: Wago and w2c2 Clang

Measured locally on Apple M4 Max (darwin/arm64), 2026-10-13, 21:07–21:28 UTC. All 118 feature workloads in the local corpus were run for both engines: 236 snapshots, four phases, three launches × five samples per phase, memory/RSS and code measurements. The shared device lock covered collection. No snapshots were published to either production or the local database.

Wago source: `d9985d87a1d5180ce618356ded517ed6b13e02c6`, branch `jairus/wasm3-feature-parity`. Frozen committed source excludes the unfinished pinned-counter experiment. Peer source: `f303ed27a8673d8cb7a6b7c39dd72a57a3b9999b` (JairusSW/w2c2 SIMD fork). Go 1.27.1; Apple Clang 21.0.0. Peer flags: `-O3 -march=native -fno-strict-aliasing -DWASM_THREADS_PTHREADS -pthread -DW2C2_RUNTIME_CHECKS=0`.

| Engine | Successful features | Unsupported | Failed |
| --- | ---: | ---: | ---: |
| Wago | 107 | 11 | 0 |
| w2c2 Clang | 54 | 35 | 29 |

All 29 failed cases match failed workloads and phases from the previous full suite. There are zero resource-collection failures on successful timing rows. Failed/unsupported outcomes and their resource statuses remain in the raw report.

Ratios below are Wago / w2c2 Clang on exact matching contracts and artifact hashes; lower is faster. Each phase has 54 successful matched pairs.

| Phase | Geometric mean ratio | Wago slower pairs |
| --- | ---: | ---: |
| compile | 0.000094 | 0 |
| instantiate | 1.598567 | 48 |
| first-call | 0.416434 | 15 |
| steady | 0.364773 | 15 |

The compile phase compares JIT compilation against translation plus native Clang compilation, with different work and amortization. A favorable aggregate does not establish feature-by-feature parity. Fifteen steady-state features remain slower, including ten more than 10% slower.

| Slower steady-state feature | Wago µs | w2c2 Clang µs | Ratio |
| --- | ---: | ---: | ---: |
| features/threads/host-opaque-identity/performance-4096x64 | 11083.312 | 193.532 | 57.269 |
| features/compact-imports/ordinary-baseline/performance-4096x64 | 1067.760 | 298.308 | 3.579 |
| features/threads/host-identity/performance-4096x64 | 608.269 | 194.705 | 3.124 |
| features/core-tab/indirect-call/performance-4096x64 | 332.477 | 187.490 | 1.773 |
| features/saturating-float-to-int/f32-in-range/performance-4096x64 | 81.184 | 53.305 | 1.523 |
| features/bulk-memory/fill/performance-65536x64 | 48.004 | 32.590 | 1.473 |
| features/mutable-globals/f64-global-scale/performance-4096x64 | 138.034 | 102.921 | 1.341 |
| features/core-glb/f64-global-scale/performance-4096x64 | 135.227 | 101.837 | 1.328 |
| features/bulk-memory/copy-overlap/performance-65536x64 | 96.022 | 74.539 | 1.288 |
| features/simd/f32x4-sqrt/performance-4096x64 | 247.525 | 220.678 | 1.122 |
| features/core-glb/dependent-read-write/performance-4096x64 | 132.853 | 123.381 | 1.077 |
| features/mutable-globals/dependent-read-write/performance-4096x64 | 134.940 | 126.599 | 1.066 |
| features/simd/swizzle-lanes/performance-4096x64 | 78.772 | 77.047 | 1.022 |
| features/core-mem/load-store-unaligned/performance-4096x64 | 100.803 | 100.295 | 1.005 |
| features/core-num/f64-sqrt-convert/performance-4096x64 | 203.136 | 202.747 | 1.002 |

[Full report, capture manifest, raw samples, statuses and comparison](full-feature-perf-d9985d87.json). Raw local captures are retained in `.validation/wasm3-parity/full-feature-d9985d87/captures`.
