# Checked compact guarded range diagnostic

Runtime source `d0c35039`, driver `980fde6f`. Baseline checked paired-loop + refstruct versus identical source adding guarded range mode; shared checks OFF both. Explicit 2 MiB Eden and two 8 MiB old semispaces, allocation elision disabled. Sequential builds and exclusive worker timing window; no CPU affinity.

All 12 separate 128-warmup actual-GC audits and 36 bounded processes passed exact checksums/allocation counts. Bounded protocol: three warmups, five timed calls, three rotating fresh processes. Short copy measurements allow zero collections, separate audits do not.

| Case | Baseline ms (range) | Range ms (range) | Ratio |
|---|---:|---:|---:|
| struct-allocation-access | 6.6432 (6.0352–7.2679) | 6.0338 (5.6466–6.4564) | 0.908× |
| array-allocation-access | 12.3890 (11.9856–16.0154) | 13.0724 (13.0065–18.1205) | 1.055× |
| array-fill | 23.6275 (20.9238–65.2721) | 16.5600 (13.4777–17.8031) | 0.701× |
| array-copy-checksum | 0.3513 (0.3456–0.4285) | 0.4363 (0.3788–0.4745) | 1.242× |
| array-loop-copy-checksum | 0.6520 (0.5574–0.6828) | 0.4255 (0.3650–0.4586) | 0.653× |
| tree-survival | 1.9953 (1.8450–2.0499) | 2.1351 (2.0291–2.3163) | 1.070× |

Scalar copy improves in every paired round. Bulk-copy and tree controls regress in every paired round; the baseline fill 65.27 ms outlier is retained. Fill native code is unchanged, so its apparent improvement cannot be attributed to new range instructions. Preserve these controls. Shared-host variance remains large; no universal gain follows.

## Separate compile footprint

256 Compile+Close iterations per case, three rotating processes. Process medians:

| Case | Baseline/range µs | Code bytes | Go bytes | Allocations |
|---|---:|---:|---:|---:|
| struct-allocation-access | 167.69/180.86 | 3183/3183 | 68131/68131 | 253/253 |
| array-allocation-access | 166.66/173.56 | 3439/3439 | 71987/71987 | 242/242 |
| array-fill | 202.92/198.83 | 3951/3951 | 76299/76299 | 246/246 |
| array-copy-checksum | 214.12/262.96 | 7599/7663 | 82852/82852 | 289/289 |
| array-loop-copy-checksum | 233.82/229.40 | 7599/7663 | 82852/82852 | 289/289 |
| tree-survival | 191.88/187.00 | 4015/4015 | 69731/69731 | 289/289 |

Both copy artifacts add 64 native bytes, with unchanged Go compile allocation bytes and counts. Other code sizes are unchanged. Compile timings are noisy, not evidence of faster compilation.

Timed collection counts match variants: struct 13 minor/0 major, array and fill 51/2, copies 0/0, tree 3/0. Exact allocations per five-call batch: 1,311,040 for struct/array/fill, 640 for copies, 337,755 for tree. Raw JSON retains counters, Go memory, binary/artifact hashes and post-run RSS. Ignore inherited maxrss_kib; copied/promoted/scan counters are not instrumented.

## Separate full scalar-copy V8 gate

Original 100 warmups + nine batches of 32 calls, three rotating processes: range **0.4383 ms** versus V8 **0.1474 ms**, **2.974×**, **MISS** against 1.15×. Candidate per-process medians range 0.4373–0.4572 ms; retain the slower round. Node 24.22.1 / V8 13.6.233.17-node.58, --no-liftoff. Exact 36,864 timed allocations and 3 minor/0 major collections each run. No allocation-elision claim about V8 internals. This engine comparison does not isolate collector efficiency; earlier all-six target remains failed.

Worker qualification on implementation 0507cea1 (integrated eed60aa7) included exact-artifact backend telemetry proving scalar function 2 is selected once and bulk function 1 zero times. Both artifacts contain both kernels, so both grow even though the bulk performance export does not execute scalar copy. Timed binaries exclude telemetry. No altered bulk-instruction explanation is asserted for the control regression.
