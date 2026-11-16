# Fresh same-binary equal-budget age2 comparison

Qualified runtime `5dfe7d3ca8ed2528c6e7a5fed73a3ea4acd8ef06`, probe 7e0edee4/5d15fd6b, full driver 3b56a63e. One identical binary for baseline and age2, tags `wago_gc_compact_range_sse2,wago_gc_compact_refstruct,wago_gc_compact_age2`, runtime ExperimentalCompactAge2 false/true. Diagnostics, weighted reconstruction, stride and shared checks OFF; allocation elision OFF. New Intel Xeon Platinum 8573C host, Go 1.27.1, Node 24.22.1 / V8 13.6.233.17-node.58 --no-liftoff. Historical AMD timings are not comparators.

## Policy and memory geometry

Equal total young backing 2 MiB and two 8 MiB old semispaces. Baseline active nursery 2 MiB; age2 active 1 MiB +inactive 1 MiB, survivor cap 512 KiB. Oversized allocation threshold changes 2 MiB→1 MiB. Thus this does not hold active nursery capacity or oversized classification constant.

Independent equivalent descriptors report identical fixed backing 28,463,151 bytes: arena 18,874,399 (includes null/alignment slack), starts 2,359,298, dirty 2,359,298, marks 147,464, queue 4,718,596, remembered 4,096. Collector 248 bytes, descriptors 144 and reference offsets 8 additionally. This substrate accounting excludes runtime adapter/compiled-module overhead; actual per-process Go heap/RSS follow below. No diagnostic scratch is included.

## Full fresh V8 gate

All 12 actual-GC audits, 36 bounded processes and 54 full-protocol processes pass checksums and exact allocation counts. Full protocol remains 100 separate warmups, 9 batches of 32 calls, 3 rotating fresh processes per case/variant; GOMAXPROCS 1, no CPU affinity, exclusive worker window. V8 internal allocations/elision are not observed; this is engine-level comparison, not isolated collector efficiency.

| Case | Baseline ms | Age2 ms | V8 ms | Age2/baseline | Age2/V8 | Gate |
|---|---:|---:|---:|---:|---:|---|
| struct-allocation-access | 8.6011 | 8.4577 | 1.2903 | 0.983× | 6.555× | MISS |
| array-allocation-access | 16.4189 | 28.1046 | 8.0674 | 1.712× | 3.484× | MISS |
| array-fill | 19.4025 | 28.7762 | 9.5853 | 1.483× | 3.002× | MISS |
| array-copy-checksum | 0.3548 | 0.3275 | 0.1623 | 0.923× | 2.017× | MISS |
| array-loop-copy-checksum | 0.3824 | 0.3706 | 0.1633 | 0.969× | 2.269× | MISS |
| tree-survival | 2.5850 | 2.6680 | 0.8390 | 1.032× | 3.180× | MISS |

Every target case misses 1.15×. Arrays/fill materially regress despite eliminating major collections. Do not enable age2 by default on this evidence. Other differences are smaller/noisier and do not rescue the policy. Increased minor frequency and halved active capacity are observed; precise copying/promotion costs are not instrumented, so do not infer them from zero generic counters.

| Case | Baseline minor/full | Age2 minor/full | Baseline/age2 Go HeapAlloc MiB | Baseline/age2 process HWM MiB |
|---|---:|---:|---:|---:|
| struct-allocation-access | 716/7 | 1536/0 | 27.50/27.60 | 38.79/18.94 |
| array-allocation-access | 2901/125 | 9216/0 | 27.80/28.54 | 39.12/19.95 |
| array-fill | 2901/125 | 9216/0 | 27.80/28.54 | 39.54/19.91 |
| array-copy-checksum | 3/0 | 5/0 | 27.41/27.41 | 18.37/18.38 |
| array-loop-copy-checksum | 3/0 | 5/0 | 27.41/27.41 | 18.32/18.44 |
| tree-survival | 147/1 | 306/0 | 27.48/27.47 | 31.13/19.36 |

Collections are over 288 timed calls. Counts match all three rounds. Ignore inherited maxrss_kib and use /proc process HWM. RSS/Go heap reflect the actual runtime, but short process peaks and OS residency do not establish a universal memory saving.

## Bounded preliminary comparison

Three warmups/five timed calls/three rotating processes, separate from steady-state results:

| Case | Baseline ms range | Age2 ms range |
|---|---:|---:|
| struct-allocation-access | 8.0952 (7.9419–9.8504) | 11.1953 (8.7491–12.4966) |
| array-allocation-access | 19.6913 (19.5673–23.2275) | 31.2917 (28.4031–31.5200) |
| array-fill | 29.7655 (25.7652–31.5844) | 53.5702 (39.0922–74.4709) |
| array-copy-checksum | 0.3858 (0.3816–1.4634) | 0.4598 (0.4370–0.4946) |
| array-loop-copy-checksum | 0.3096 (0.3090–0.4149) | 0.3640 (0.3557–0.3746) |
| tree-survival | 2.4139 (2.2121–2.9276) | 2.5546 (2.3408–2.8072) |

The initial struct/array/fill regressions occurred in every paired bounded round; the longer struct result is closer, demonstrating why the short diagnostic is not a universal acceptance gate. All outliers are preserved.

## Compile footprint

Same compiler/binary means no policy-specific compile difference. Separate 256-iteration Compile+Close measurements in 3 processes:

| Case | Median µs | Native bytes | Go bytes/op | Allocations/op |
|---|---:|---:|---:|---:|
| struct-allocation-access | 268.31 | 3183 | 68131 | 253 |
| array-allocation-access | 246.64 | 3439 | 71987 | 242 |
| array-fill | 287.59 | 3951 | 76300 | 246 |
| array-copy-checksum | 516.69 | 7727 | 82852 | 289 |
| array-loop-copy-checksum | 319.54 | 7727 | 82852 | 289 |
| tree-survival | 251.18 | 4015 | 69731 | 289 |

Raw evidence includes source/binary/artifact hashes, every process/sample, config and memory. Untimed geometry is independent substrate evidence, not a new public runtime API. All existing full-GC correctness qualification preceded these runs; benchmark passes alone do not prove complete GC correctness.
