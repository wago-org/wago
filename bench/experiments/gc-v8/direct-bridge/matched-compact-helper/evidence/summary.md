# Compact versus direct16 helper-only comparison

Measured source `2c14fd9b`, runtime `62fc59f3`. Same-source binaries use explicit direct16 helper-only versus compact helper-only configuration. Neither uses optimized native direct code. Explicit 2 MiB Eden and two 8 MiB old semispaces. All 12 audits and 36 timing processes passed exact artifact hashes, checksums, expected object allocations and real collection checks. Audits use 128 warmups; timings use 100 warmups, nine batches of 32 calls and three rotating process rounds.

| Case | Backend | Median ms | Process medians ms | Timed minor/full | Go heap MiB | Peak RSS MiB |
|---|---|---:|---|---|---:|---:|
| struct-allocation-access | direct16 | 144.022 | 147.142, 144.022, 138.696 | 1275/23 | 27.58 | 39.70 |
| struct-allocation-access | compact | 180.032 | 180.032, 187.917, 177.873 | 716/7 | 27.50 | 39.61 |
| array-allocation-access | direct16 | 157.163 | 155.922, 157.163, 160.014 | 2901/125 | 27.80 | 39.73 |
| array-allocation-access | compact | 207.330 | 208.414, 207.330, 206.579 | 2901/125 | 27.80 | 39.89 |
| array-fill | direct16 | 208.633 | 208.633, 207.859, 213.885 | 2901/125 | 27.80 | 39.73 |
| array-fill | compact | 284.614 | 280.042, 284.614, 286.200 | 2901/125 | 27.80 | 39.92 |
| array-copy-checksum | direct16 | 19.748 | 19.748, 20.228, 18.861 | 3/0 | 27.41 | 18.69 |
| array-copy-checksum | compact | 33.747 | 34.067, 33.747, 33.289 | 3/0 | 27.41 | 18.77 |
| array-loop-copy-checksum | direct16 | 56.029 | 56.029, 56.866, 55.210 | 3/0 | 27.41 | 18.95 |
| array-loop-copy-checksum | compact | 93.756 | 93.756, 94.938, 91.285 | 3/0 | 27.41 | 18.95 |
| tree-survival | direct16 | 42.549 | 42.386, 47.238, 42.549 | 293/3 | 27.55 | 39.20 |
| tree-survival | compact | 53.262 | 50.853, 54.165, 53.262 | 147/1 | 27.48 | 31.76 |

## Interpretation

Compact is slower on every workload: every compact process median exceeds every direct16 process median. Smaller struct geometry reduces collection counts, but does not compensate for current helper/adapter costs. Descriptor/layout overhead is a plausible explanation, not established by this unprofiled comparison. This does not predict future compact native-code performance or meet any V8 target. The helper-only runtimes are deliberately not compared against previous optimized native pipelines.

This is a complete backend implementation comparison, NOT isolated header-size attribution: compact necessarily retires through its start map after forwarding overwrites type metadata, whereas direct16 uses header traversal. Both use exact roots, but layout/traversal/allocation behavior differs. No default adoption is supported by these timings.

## Independent physical geometry

Equivalent substrate descriptors at the same configured capacities report identical fixed backing: **28,463,151 bytes** each. This excludes type descriptors, adapter state and Go overhead. Physical object geometry:

| Shape | Direct extent / stride | Compact payload offset / extent / stride |
|---|---|---|
| i32 struct | 24 / 32 | 4 / 8 / 16 |
| i32[16] array | 80 / 80 | 8 / 72 / 80 |
| i32 + two-reference tree node | 32 / 32 | 4 / 16 / 16 |

Reduced header bytes do not necessarily reduce stride: the array remains 80 bytes. Fixed preallocated arena/tracing backing does not shrink automatically with smaller objects. Runtime Go heaps remain nearly equal. Lower tree RSS reflects resident pages/collection history, not a reduced configured backing budget. Geometry was measured separately using immutable equivalent descriptors, not unsafe adapter reflection.

## Separate Compile+Close footprint

| Case | Backend | Median us/op | Native bytes | Go B/op | Allocs/op |
|---|---|---:|---:|---:|---:|
| struct-allocation-access | direct16 | 157.576 | 1391 | 51466 | 223 |
| struct-allocation-access | compact | 131.976 | 1391 | 51466 | 223 |
| array-allocation-access | direct16 | 130.258 | 1391 | 51434 | 213 |
| array-allocation-access | compact | 131.968 | 1391 | 51434 | 213 |
| array-fill | direct16 | 131.561 | 1519 | 52058 | 215 |
| array-fill | compact | 127.278 | 1519 | 52058 | 215 |
| array-copy-checksum | direct16 | 167.466 | 2287 | 59251 | 245 |
| array-copy-checksum | compact | 156.173 | 2287 | 59251 | 245 |
| array-loop-copy-checksum | direct16 | 161.233 | 2287 | 59251 | 245 |
| array-loop-copy-checksum | compact | 153.784 | 2287 | 59251 | 245 |
| tree-survival | direct16 | 136.173 | 1135 | 52858 | 256 |
| tree-survival | compact | 134.800 | 1135 | 52858 | 256 |

Compile measurements are separate from execution and use 256 Compile+Close iterations per case, three rotating processes. They are not peak compiler-memory measurements. All raw output, binary hashes, configurations and counters are retained. Profiling/builds by other workers were excluded during timing; shared-host frequency/scheduling remain uncontrolled. Direct copied/promoted/scan counters are uninstrumented.
