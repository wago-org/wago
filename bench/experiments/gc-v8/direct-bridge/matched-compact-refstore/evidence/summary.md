# Bounded compact reference-store diagnostic

Measured source `285c05f7`, runtime `aa519cb7`, Same-source compact native allocation versus added checked reference-array stores; numeric-array access/allocation, fill and copy remain helper paths. Explicit 2 MiB Eden and two 8 MiB old semispaces. All 12 separate 128-warmup real-collection audits and 36 bounded diagnostic processes passed. Timed diagnostic protocol is THREE warmup calls plus FIVE timed calls, three rotating fresh-process rounds, not the historical full protocol. Copy diagnostics explicitly permit zero collection only after the separate audited collection run. Exact hashes, checksums and allocation counts still apply.

| Case | Mode | Median ms | Samples ms | Timed minor/full | Go heap MiB | RSS MiB |
|---|---|---:|---|---|---:|---:|
| struct-allocation-access | alloc | 68.799 | 68.799, 70.516, 62.275 | 13/0 | 27.42 | 20.11 |
| struct-allocation-access | refstore | 5.938 | 6.050, 5.793, 5.938 | 13/0 | 27.42 | 20.24 |
| array-allocation-access | alloc | 211.865 | 220.817, 211.865, 211.181 | 51/2 | 27.42 | 38.30 |
| array-allocation-access | refstore | 136.716 | 155.014, 128.179, 136.716 | 51/2 | 27.42 | 38.36 |
| array-fill | alloc | 282.500 | 282.500, 281.636, 283.056 | 51/2 | 27.42 | 38.30 |
| array-fill | refstore | 200.669 | 220.386, 200.669, 196.184 | 51/2 | 27.42 | 38.44 |
| array-copy-checksum | alloc | 28.836 | 27.358, 31.511, 28.836 | 0/0 | 27.41 | 15.94 |
| array-copy-checksum | refstore | 27.562 | 27.562, 29.399, 26.312 | 0/0 | 27.41 | 16.12 |
| array-loop-copy-checksum | alloc | 89.875 | 89.875, 94.019, 81.418 | 0/0 | 27.41 | 16.00 |
| array-loop-copy-checksum | refstore | 81.294 | 77.531, 85.233, 81.294 | 0/0 | 27.41 | 16.30 |
| tree-survival | alloc | 38.244 | 38.244, 37.791, 39.920 | 3/0 | 27.41 | 18.36 |
| tree-survival | refstore | 41.803 | 45.273, 40.511, 41.803 | 3/0 | 27.41 | 18.57 |

## Interpretation

Struct, array and fill improve consistently: every refstore sample is below every alloc-only sample. Medians are 68.799 to 5.938, 211.865 to 136.716 and 282.500 to 200.669 ms. Code sizes grow 2415 to 3183, 1391 to 2223 and 1519 to 2351 bytes. Copy and tree code sizes/compile allocation footprints are unchanged. Copy samples overlap; tree is slower in every candidate sample, with unknown cause. Do not attribute unchanged-code controls to selected refstore instructions. No broad gain or V8 target claim.

## Selection scope

Code-size changes establish selection on the three benefiting artifacts. Separately qualified source tests cover wrong-child rejection, clean-old helper fallback, dirty-old native stores, SIMD and register pressure. These are not proof every store in every case avoids helpers. Numeric-array native paths remain excluded.

## Separate Compile+Close footprint

| Case | Mode | Median us/op | Code bytes | Go B/op | Allocs/op |
|---|---|---:|---:|---:|---:|
| struct-allocation-access | alloc | 149.945 | 2415 | 58867 | 241 |
| struct-allocation-access | refstore | 179.206 | 3183 | 68067 | 250 |
| array-allocation-access | alloc | 140.330 | 1391 | 51434 | 213 |
| array-allocation-access | refstore | 188.034 | 2223 | 56939 | 222 |
| array-fill | alloc | 138.672 | 1519 | 52058 | 215 |
| array-fill | refstore | 154.541 | 2351 | 58075 | 224 |
| array-copy-checksum | alloc | 166.609 | 2287 | 59251 | 245 |
| array-copy-checksum | refstore | 172.824 | 2287 | 59251 | 245 |
| array-loop-copy-checksum | alloc | 177.313 | 2287 | 59251 | 245 |
| array-loop-copy-checksum | refstore | 183.995 | 2287 | 59251 | 245 |
| tree-survival | alloc | 148.163 | 1455 | 58427 | 269 |
| tree-survival | refstore | 159.421 | 1455 | 58427 | 269 |

Compile probes are separate, 256 Compile+Close operations per case and three rotating processes. They are not peak-memory measurements. No V8 runs were included. Full target acceptance requires a later full-protocol sufficiently-native pipeline; this diagnostic cannot supply it. Worker CPU activity was excluded, but shared-host frequency/scheduling remain uncontrolled. Exact configurations, counters, hashes and raw outputs are retained.
