# Bounded compact native-read diagnostic

Measured source `4fc3bbbc`, runtime `b7b6c3b2`, including Payload single-resolution fix in BOTH variants. Same-source compact helpers versus checked compact native reads; allocation/stores/fill/copy remain helper paths. Explicit 2 MiB Eden and two 8 MiB old semispaces. All 12 separate 128-warmup real-collection audits and 36 bounded diagnostic processes passed. Timed diagnostic protocol is THREE warmup calls plus FIVE timed calls, three rotating fresh-process rounds, not the historical full protocol. Copy diagnostics explicitly permit zero collection only after the separate audited collection run. Exact hashes, checksums and allocation counts still apply.

| Case | Mode | Median ms | Samples ms | Timed minor/full | Go heap MiB | RSS MiB |
|---|---|---:|---|---|---:|---:|
| struct-allocation-access | helpers | 168.761 | 168.761, 172.622, 163.973 | 13/0 | 27.42 | 20.18 |
| struct-allocation-access | reads | 133.421 | 141.592, 130.832, 133.421 | 13/0 | 27.42 | 20.10 |
| array-allocation-access | helpers | 194.605 | 202.185, 194.605, 191.528 | 51/2 | 27.42 | 37.95 |
| array-allocation-access | reads | 211.246 | 211.246, 212.924, 204.427 | 51/2 | 27.42 | 38.21 |
| array-fill | helpers | 254.971 | 281.915, 249.126, 254.971 | 51/2 | 27.42 | 38.08 |
| array-fill | reads | 272.011 | 273.855, 267.561, 272.011 | 51/2 | 27.42 | 38.40 |
| array-copy-checksum | helpers | 27.923 | 27.923, 26.379, 28.482 | 0/0 | 27.41 | 15.65 |
| array-copy-checksum | reads | 29.508 | 29.508, 29.579, 26.889 | 0/0 | 27.41 | 15.98 |
| array-loop-copy-checksum | helpers | 77.227 | 85.832, 76.087, 77.227 | 0/0 | 27.41 | 15.80 |
| array-loop-copy-checksum | reads | 79.468 | 79.468, 82.734, 79.383 | 0/0 | 27.41 | 16.04 |
| tree-survival | helpers | 48.056 | 48.597, 48.056, 45.064 | 3/0 | 27.41 | 18.16 |
| tree-survival | reads | 39.245 | 39.245, 39.357, 39.050 | 3/0 | 27.41 | 18.42 |

## Interpretation

Struct and tree improve consistently: every native-read sample is below every helper sample. Array allocation regresses consistently: every native-read sample exceeds every helper sample. Fill and both copy cases overlap or have mixed directions. Do not claim a broad gain or infer V8 parity. The stage selectively removes supported struct reads, not the remaining helper-heavy paths; these bounded timings are for intermediate attribution only. Future native allocation/store work needs its own qualification and evidence.

## Separate Compile+Close footprint

| Case | Mode | Median us/op | Code bytes | Go B/op | Allocs/op |
|---|---|---:|---:|---:|---:|
| struct-allocation-access | helpers | 176.439 | 1391 | 51466 | 223 |
| struct-allocation-access | reads | 187.446 | 2031 | 56978 | 235 |
| array-allocation-access | helpers | 146.825 | 1391 | 51434 | 213 |
| array-allocation-access | reads | 133.343 | 1391 | 51434 | 213 |
| array-fill | helpers | 138.547 | 1519 | 52058 | 215 |
| array-fill | reads | 128.102 | 1519 | 52058 | 215 |
| array-copy-checksum | helpers | 173.302 | 2287 | 59251 | 245 |
| array-copy-checksum | reads | 169.986 | 2287 | 59251 | 245 |
| array-loop-copy-checksum | helpers | 160.035 | 2287 | 59251 | 245 |
| array-loop-copy-checksum | reads | 176.812 | 2287 | 59251 | 245 |
| tree-survival | helpers | 134.939 | 1135 | 52858 | 256 |
| tree-survival | reads | 170.338 | 1455 | 58331 | 267 |

Compile probes are separate, 256 Compile+Close operations per case and three rotating processes. They are not peak-memory measurements. No V8 runs were included. Full target acceptance requires a later full-protocol sufficiently-native pipeline; this diagnostic cannot supply it. Worker CPU activity was excluded, but shared-host frequency/scheduling remain uncontrolled. Exact configurations, counters, hashes and raw outputs are retained.
