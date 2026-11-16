# Checked compact strength-reduced range diagnostic

Runtime source `6237226a`, driver `4625d9b0`. Baseline checked range-loop + refstruct versus identical source adding strength-reduced range mode; shared checks OFF both. Explicit 2 MiB Eden and two 8 MiB old semispaces, allocation elision disabled. Sequential builds and exclusive worker timing window; no CPU affinity.

All 12 separate 128-warmup actual-GC audits and 36 bounded processes passed exact checksums/allocation counts. Bounded protocol: three warmups, five timed calls, three rotating fresh processes. Short copy measurements allow zero collections, separate audits do not.

| Case | Baseline ms (range) | Stride ms (range) | Ratio |
|---|---:|---:|---:|
| struct-allocation-access | 6.5080 (6.3528–6.6542) | 6.3608 (5.6124–6.6268) | 0.977× |
| array-allocation-access | 13.1321 (12.6664–20.6573) | 13.3140 (12.5666–14.7138) | 1.014× |
| array-fill | 17.6540 (17.4459–17.9332) | 17.5901 (15.8415–18.8844) | 0.996× |
| array-copy-checksum | 0.4642 (0.3586–0.4850) | 0.3968 (0.3716–0.5447) | 0.855× |
| array-loop-copy-checksum | 0.4441 (0.3486–0.6275) | 0.4652 (0.4183–2.5745) | 1.047× |
| tree-survival | 2.1400 (1.9792–2.2308) | 2.2296 (2.1600–2.2393) | 1.042× |

No measured scalar-copy gain is established. The candidate has a 2.5745 ms bounded outlier, retained rather than discarded. Controls overlap substantially. Keep this experimental optimization disabled absent stronger evidence.

## Separate compile footprint

256 Compile+Close iterations per case, three rotating processes. Process medians:

| Case | Baseline/stride µs | Code bytes | Go bytes | Allocations |
|---|---:|---:|---:|---:|
| struct-allocation-access | 252.35/232.06 | 3183/3183 | 68131/68131 | 253/253 |
| array-allocation-access | 201.40/185.00 | 3439/3439 | 71987/71987 | 242/242 |
| array-fill | 197.79/225.92 | 3951/3951 | 76299/76299 | 246/246 |
| array-copy-checksum | 220.88/216.94 | 7663/7663 | 82852/82852 | 289/289 |
| array-loop-copy-checksum | 224.91/223.38 | 7663/7663 | 82852/82852 | 289/289 |
| tree-survival | 169.54/179.03 | 4015/4015 | 69731/69731 | 289/289 |

All native code sizes remain unchanged, including 7,663 bytes for both copy artifacts. Go compile allocation counts are unchanged; one-byte averaging differences are not a meaningful footprint change. Compile timings are noisy, not evidence of faster compilation.

Timed collection counts match variants: struct 13 minor/0 major, array and fill 51/2, copies 0/0, tree 3/0. Exact allocations per five-call batch: 1,311,040 for struct/array/fill, 640 for copies, 337,755 for tree. Raw JSON retains counters, Go memory, binary/artifact hashes and post-run RSS. Ignore inherited maxrss_kib; copied/promoted/scan counters are not instrumented.

## Separate full scalar-copy V8 gate

Original 100 warmups + nine batches of 32 calls, three rotating processes: stride **0.4443 ms** versus V8 **0.1521 ms**, **2.922×**, **MISS** against 1.15×. Candidate per-process medians range 0.4413–0.5019 ms; retain the slower round. Node 24.22.1 / V8 13.6.233.17-node.58, --no-liftoff. Exact 36,864 timed allocations and 3 minor/0 major collections each run. No allocation-elision claim about V8 internals. This engine comparison does not isolate collector efficiency; earlier all-six target remains failed.

Worker qualification on cbbb8b0b plus 237bca0a (integrated d635fb95/6237226a) proves gc-compact-range-stride selection once in exact scalar function 2, with large-end fallback covered. Timed binaries exclude telemetry. Equal code size is not identical instruction bytes. Cohort diagnostic 38c4fb7a is excluded. This evidence does not justify default enablement.
