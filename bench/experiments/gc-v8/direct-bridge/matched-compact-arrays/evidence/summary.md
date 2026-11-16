# Bounded compact numeric-array diagnostic

Measured source `58474bba`, runtime `1cf7b6ab`, Same-source compact refstore versus added numeric-array access then native numeric allocation. Fill/copy helpers and reference-struct getter/constructor paths remain incomplete. Explicit 2 MiB Eden and two 8 MiB old semispaces. All 18 separate 128-warmup real-collection audits and 54 bounded diagnostic processes passed. Timed diagnostic protocol is THREE warmup calls plus FIVE timed calls, three rotating fresh-process rounds, not the historical full protocol. Copy diagnostics explicitly permit zero collection only after the separate audited collection run. Exact hashes, checksums and allocation counts still apply.

| Case | Mode | Median ms | Samples ms | Timed minor/full | Go heap MiB | RSS MiB |
|---|---|---:|---|---|---:|---:|
| struct-allocation-access | refstore | 6.368 | 6.368, 6.247, 6.559 | 13/0 | 27.42 | 20.24 |
| struct-allocation-access | scalararrays | 6.313 | 8.294, 6.100, 6.313 | 13/0 | 27.42 | 20.08 |
| struct-allocation-access | arrayalloc | 6.406 | 6.152, 6.732, 6.406 | 13/0 | 27.42 | 20.20 |
| array-allocation-access | refstore | 141.783 | 140.714, 143.684, 141.783 | 51/2 | 27.42 | 38.36 |
| array-allocation-access | scalararrays | 93.554 | 100.328, 93.554, 83.177 | 51/2 | 27.42 | 38.31 |
| array-allocation-access | arrayalloc | 13.032 | 16.530, 13.032, 12.806 | 51/2 | 27.42 | 38.34 |
| array-fill | refstore | 217.673 | 201.289, 217.673, 249.310 | 51/2 | 27.42 | 38.50 |
| array-fill | scalararrays | 174.139 | 157.208, 174.139, 185.488 | 51/2 | 27.42 | 38.18 |
| array-fill | arrayalloc | 76.280 | 78.617, 71.571, 76.280 | 51/2 | 27.42 | 38.46 |
| array-copy-checksum | refstore | 30.142 | 27.574, 30.142, 33.839 | 0/0 | 27.41 | 16.28 |
| array-copy-checksum | scalararrays | 2.165 | 2.165, 2.353, 1.937 | 0/0 | 27.41 | 15.97 |
| array-copy-checksum | arrayalloc | 1.991 | 1.991, 2.177, 1.833 | 0/0 | 27.41 | 16.24 |
| array-loop-copy-checksum | refstore | 89.210 | 81.124, 89.210, 92.828 | 0/0 | 27.41 | 16.30 |
| array-loop-copy-checksum | scalararrays | 1.980 | 1.980, 4.693, 1.907 | 0/0 | 27.41 | 15.90 |
| array-loop-copy-checksum | arrayalloc | 2.317 | 1.971, 2.628, 2.317 | 0/0 | 27.41 | 16.20 |
| tree-survival | refstore | 39.442 | 39.442, 48.702, 39.095 | 3/0 | 27.41 | 18.48 |
| tree-survival | scalararrays | 45.235 | 39.317, 58.743, 45.235 | 3/0 | 27.41 | 18.39 |
| tree-survival | arrayalloc | 43.051 | 43.051, 53.708, 38.760 | 3/0 | 27.41 | 18.56 |

## Interpretation

Numeric-array access gives clear gains on array/fill and both copy kernels; native array allocation then gives clear additional gains on array/fill. Array medians are 141.783 to 93.554 to 13.032 ms; fill 217.673 to 174.139 to 76.280 ms. Copy changes 30.142 to 2.165 to 1.991 ms, while scalar-copy changes 89.210 to 1.980 to 2.317 ms. The latter two allocator-stage differences overlap and do not establish an additional gain. Struct/tree are noisy overlapping controls with unchanged code size and compile allocation footprint; no control speedup or mechanism claim.

## Selection and cost

Struct remains 3183 code bytes and tree 1455 across all modes. Array grows 2223 to 2991 to 3439, fill 2351 to 3119 to 3567, both copy modules 2287 to 4591 to 6639. These changes confirm selected numeric paths but increase compile/code footprint. Full per-case allocations below must accompany execution claims. This is bounded intermediate attribution, not a V8 gate or universal gain.

## Separate Compile+Close footprint

| Case | Mode | Median us/op | Code bytes | Go B/op | Allocs/op |
|---|---|---:|---:|---:|---:|
| struct-allocation-access | refstore | 163.344 | 3183 | 68067 | 250 |
| struct-allocation-access | scalararrays | 181.492 | 3183 | 68067 | 250 |
| struct-allocation-access | arrayalloc | 178.125 | 3183 | 68067 | 250 |
| array-allocation-access | refstore | 172.843 | 2223 | 56939 | 222 |
| array-allocation-access | scalararrays | 150.471 | 2991 | 61555 | 237 |
| array-allocation-access | arrayalloc | 169.755 | 3439 | 71987 | 242 |
| array-fill | refstore | 153.820 | 2351 | 58075 | 224 |
| array-fill | scalararrays | 150.817 | 3119 | 63203 | 239 |
| array-fill | arrayalloc | 202.653 | 3567 | 73507 | 244 |
| array-copy-checksum | refstore | 184.216 | 2287 | 59251 | 245 |
| array-copy-checksum | scalararrays | 184.739 | 4591 | 76948 | 276 |
| array-copy-checksum | arrayalloc | 216.973 | 6639 | 76308 | 286 |
| array-loop-copy-checksum | refstore | 187.181 | 2287 | 59251 | 245 |
| array-loop-copy-checksum | scalararrays | 187.071 | 4591 | 76948 | 276 |
| array-loop-copy-checksum | arrayalloc | 216.787 | 6639 | 76308 | 286 |
| tree-survival | refstore | 161.692 | 1455 | 58427 | 269 |
| tree-survival | scalararrays | 160.386 | 1455 | 58427 | 269 |
| tree-survival | arrayalloc | 156.133 | 1455 | 58427 | 269 |

Compile probes are separate, 256 Compile+Close operations per case and three rotating processes. They are not peak-memory measurements. No V8 runs were included. Full target acceptance requires a later full-protocol sufficiently-native pipeline; this diagnostic cannot supply it. Worker CPU activity was excluded, but shared-host frequency/scheduling remain uncontrolled. Exact configurations, counters, hashes and raw outputs are retained.
