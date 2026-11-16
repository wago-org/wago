# Checked compact paired-loop diagnostic

Runtime source `7483b3e0`, driver `a36a4783`. Baseline checked compact loop + refstruct versus identical source adding paired-loop mode; shared checks OFF both. Explicit 2 MiB Eden and two 8 MiB old semispaces, allocation elision disabled. Sequential builds and exclusive worker timing window; no CPU affinity.

All 12 separate 128-warmup actual-GC audits and 36 bounded processes passed exact checksums/allocation counts. Bounded protocol: three warmups, five timed calls, three rotating fresh processes. Short copy measurements allow zero collections, separate audits do not.

| Case | Baseline ms (range) | Paired ms (range) | Ratio |
|---|---:|---:|---:|
| struct-allocation-access | 6.6006 (5.8775–6.8966) | 6.1292 (6.0032–7.0036) | 0.929× |
| array-allocation-access | 15.8630 (12.9678–16.5336) | 15.4133 (12.6973–97.3734) | 0.972× |
| array-fill | 16.8183 (14.4567–17.1334) | 18.2022 (17.6492–18.2786) | 1.082× |
| array-copy-checksum | 0.3492 (0.3457–0.3731) | 0.4958 (0.3608–0.6053) | 1.420× |
| array-loop-copy-checksum | 1.6083 (1.5599–2.1351) | 0.5979 (0.5561–0.7172) | 0.372× |
| tree-survival | 2.2967 (2.2184–2.4212) | 1.9799 (1.8699–3.1184) | 0.862× |

Scalar copy improves in each paired round. Bulk-copy control regresses in each paired round; fill also regresses, and array has a 97.37 ms candidate outlier. Preserve these controls. Shared-host variance remains large; no universal gain follows.

## Separate compile footprint

256 Compile+Close iterations per case, three rotating processes. Process medians:

| Case | Baseline/paired µs | Code bytes | Go bytes | Allocations |
|---|---:|---:|---:|---:|
| struct-allocation-access | 169.94/169.96 | 3183/3183 | 68131/68131 | 253/253 |
| array-allocation-access | 161.70/175.04 | 3439/3439 | 71987/71987 | 242/242 |
| array-fill | 165.16/180.95 | 3951/3951 | 76299/76300 | 246/246 |
| array-copy-checksum | 212.24/240.32 | 7535/7599 | 82852/82852 | 289/289 |
| array-loop-copy-checksum | 211.14/200.46 | 7535/7599 | 82852/82852 | 289/289 |
| tree-survival | 180.03/202.17 | 4015/4015 | 69731/69731 | 289/289 |

Both copy artifacts add 64 native bytes, with unchanged Go compile allocation bytes and counts. Other code sizes are unchanged. Compile timings are noisy, not evidence of faster compilation.

Timed collection counts match variants: struct 13 minor/0 major, array and fill 51/2, copies 0/0, tree 3/0. Exact allocations per five-call batch: 1,311,040 for struct/array/fill, 640 for copies, 337,755 for tree. Raw JSON retains counters, Go memory, binary/artifact hashes and post-run RSS. Ignore inherited maxrss_kib; copied/promoted/scan counters are not instrumented.

## Separate full scalar-copy V8 gate

Original 100 warmups + nine batches of 32 calls, three rotating processes: paired **0.6463 ms** versus V8 **0.1444 ms**, **4.476×**, **MISS** against 1.15×. Candidate per-process medians range 0.6452–0.9544 ms; retain the slower round. Node 24.22.1 / V8 13.6.233.17-node.58, --no-liftoff. Exact 36,864 timed allocations and 3 minor/0 major collections each run. No allocation-elision claim about V8 internals. This engine comparison does not isolate collector efficiency; earlier all-six target remains failed.

Separate exact-site diagnostic commit `4152840a` confirms the new paired site is scalar function 2, loop PC 87, first access PC 97, locals 1 and 2, with eight free GP registers. Both modules contain both kernels. Bytes [0:4080], covering checksum and bulk-copy functions, are identical between baseline and paired builds; only scalar function 2 grows by 64 bytes, with the wrapper call displacement adjusted. Instrumented and uninstrumented paired native hashes match. Therefore the bulk timing regression is not explained by changed bulk/checksum instructions; its cause remains unproven. Timed binaries exclude diagnostic instrumentation.
