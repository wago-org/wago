# Bounded compact native-allocation diagnostic

Measured source `fd642bb0`, runtime `5d0d106c`, Same-source compact checked reads versus native pointer-free struct allocation; reference-containing constructors, array allocation, stores, fill and copy remain helper paths. Explicit 2 MiB Eden and two 8 MiB old semispaces. All 12 separate 128-warmup real-collection audits and 36 bounded diagnostic processes passed. Timed diagnostic protocol is THREE warmup calls plus FIVE timed calls, three rotating fresh-process rounds, not the historical full protocol. Copy diagnostics explicitly permit zero collection only after the separate audited collection run. Exact hashes, checksums and allocation counts still apply.

| Case | Mode | Median ms | Samples ms | Timed minor/full | Go heap MiB | RSS MiB |
|---|---|---:|---|---|---:|---:|
| struct-allocation-access | reads | 142.996 | 145.228, 142.996, 134.922 | 13/0 | 27.42 | 20.09 |
| struct-allocation-access | alloc | 70.845 | 72.222, 70.845, 65.103 | 13/0 | 27.42 | 20.04 |
| array-allocation-access | reads | 211.181 | 226.297, 211.181, 196.878 | 51/2 | 27.42 | 38.16 |
| array-allocation-access | alloc | 213.137 | 217.433, 213.137, 208.206 | 51/2 | 27.42 | 38.17 |
| array-fill | reads | 282.547 | 282.547, 284.223, 274.718 | 51/2 | 27.42 | 38.45 |
| array-fill | alloc | 272.585 | 272.585, 269.787, 273.363 | 51/2 | 27.42 | 38.47 |
| array-copy-checksum | reads | 29.742 | 26.286, 29.742, 34.007 | 0/0 | 27.41 | 15.89 |
| array-copy-checksum | alloc | 27.539 | 27.274, 32.316, 27.539 | 0/0 | 27.41 | 15.94 |
| array-loop-copy-checksum | reads | 80.605 | 78.946, 84.360, 80.605 | 0/0 | 27.41 | 16.05 |
| array-loop-copy-checksum | alloc | 79.654 | 79.654, 87.582, 78.102 | 0/0 | 27.41 | 15.98 |
| tree-survival | reads | 40.540 | 40.540, 41.324, 37.641 | 3/0 | 27.41 | 18.34 |
| tree-survival | alloc | 38.965 | 38.965, 39.279, 37.650 | 3/0 | 27.41 | 18.46 |

## Interpretation

Struct improves clearly: every allocator sample is below every reads-only sample, with median 142.996 to 70.845 ms. The supported struct artifact grows 2031 to 2415 code bytes, adds 1889 Go compile bytes and 6 compile allocations. All other artifact code sizes are unchanged; their timing variations must not be attributed to selected native allocations. Tree compile allocations add 2 but code size remains 1455. Controls overlap except modest fill movement despite unchanged code. No broad gain or V8 target claim is supported.

## Selection evidence

Separate TestCompactNativeGCBumpPublicationAndFallback and TestCompactNativeGCBumpBoundedShapes passed, checking untouched helper sentinels for roomy supported constructors and helper fallback for unsupported/exhausted/stale cases. These demonstrate fixture path selection, not that every corpus allocation uses the fast path. ABI3 reference stores are excluded.

## Separate Compile+Close footprint

| Case | Mode | Median us/op | Code bytes | Go B/op | Allocs/op |
|---|---|---:|---:|---:|---:|
| struct-allocation-access | reads | 130.597 | 2031 | 56978 | 235 |
| struct-allocation-access | alloc | 156.140 | 2415 | 58867 | 241 |
| array-allocation-access | reads | 125.369 | 1391 | 51434 | 213 |
| array-allocation-access | alloc | 155.830 | 1391 | 51434 | 213 |
| array-fill | reads | 125.007 | 1519 | 52058 | 215 |
| array-fill | alloc | 158.134 | 1519 | 52058 | 215 |
| array-copy-checksum | reads | 157.174 | 2287 | 59251 | 245 |
| array-copy-checksum | alloc | 175.039 | 2287 | 59251 | 245 |
| array-loop-copy-checksum | reads | 171.263 | 2287 | 59251 | 245 |
| array-loop-copy-checksum | alloc | 162.942 | 2287 | 59251 | 245 |
| tree-survival | reads | 142.546 | 1455 | 58331 | 267 |
| tree-survival | alloc | 147.915 | 1455 | 58427 | 269 |

Compile probes are separate, 256 Compile+Close operations per case and three rotating processes. They are not peak-memory measurements. No V8 runs were included. Full target acceptance requires a later full-protocol sufficiently-native pipeline; this diagnostic cannot supply it. Worker CPU activity was excluded, but shared-host frequency/scheduling remain uncontrolled. Exact configurations, counters, hashes and raw outputs are retained.
