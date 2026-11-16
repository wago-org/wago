# weighted-sse2 measured comparison

Qualified runtime: `b92d0e24f03e032c30700cea25badfd60b5c48fe`. Driver/probe-only commit is recorded in raw provenance. Same Intel Xeon Platinum 8573C host; Go 1.27.1; Node 24.22.1 / V8 13.6.233.17-node.58, `--no-liftoff`. No diagnostic tags. Both variants use identical source and the protocol tags documented in README.

All 12 actual-GC audits, 36 bounded processes, and 54 full processes passed SHA, checksum and exact Wago allocation checks. Full protocol: 100 warmup calls, nine batches of 32 calls, three rotating fresh-process rounds. Audits use 128 warmups. Bounded results use three warmups and five calls and are not pooled with full results.

**Every candidate case misses the 1.15x V8 target.** No default adoption or universal gain is justified. Values below are medians of the three process medians, in milliseconds.

| Case | Baseline | Candidate | V8 | Candidate/V8 | Candidate change |
|---|---:|---:|---:|---:|---:|
| struct-allocation-access | 7.5128 | 8.4199 | 1.3158 | 6.399 | +12.1% |
| array-allocation-access | 17.1625 | 18.6393 | 8.2295 | 2.265 | +8.6% |
| array-fill | 18.7583 | 27.8132 | 9.6848 | 2.872 | +48.3% |
| array-copy-checksum | 0.3493 | 0.2902 | 0.1593 | 1.821 | -16.9% |
| array-loop-copy-checksum | 0.2872 | 0.3351 | 0.1669 | 2.008 | +16.7% |
| tree-survival | 2.5198 | 2.4120 | 0.9138 | 2.640 | -4.3% |

## Per-round changes and bounded controls

| Case | Full changes, rounds 0/1/2 | Bounded changes, rounds 0/1/2 |
|---|---|---|
| struct-allocation-access | +12.1%, +1.4%, +16.6% | +26.9%, -11.2%, +1.6% |
| array-allocation-access | +13.0%, +36.8%, -12.6% | -24.8%, +16.8%, +27.0% |
| array-fill | +1.3%, +56.3%, +48.3% | -6.5%, +24.4%, +104.2% |
| array-copy-checksum | -15.3%, -16.9%, +0.3% | -12.7%, -17.6%, -12.3% |
| array-loop-copy-checksum | +34.5%, -11.2%, +8.2% | -6.7%, +14.2%, -58.5% |
| tree-survival | -6.6%, -7.5%, -0.8% | +10.8%, -3.9%, -31.5% |

## Allocation, collection and memory

Timing counters are real; copied/promoted and scan counters in generic Stats remain uninstrumented, so their zeros are not evidence of zero work. Each timed process makes 288 calls. No elapsed-time comparisons use diagnostic binaries.

| Case | Timed allocations | Baseline minor/full | Candidate minor/full | Go heap MiB baseline/candidate | VmHWM MiB baseline/candidate |
|---|---:|---|---|---|---|
| struct-allocation-access | 75515904 | 716/7 | 716/7 | 27.50/27.50 | 38.54/38.90 |
| array-allocation-access | 75515904 | 2901/125 | 2901/125 | 27.80/27.80 | 39.31/38.63 |
| array-fill | 75515904 | 2901/125 | 2901/125 | 27.80/27.80 | 39.27/39.21 |
| array-copy-checksum | 36864 | 3/0 | 3/0 | 27.41/27.41 | 18.25/18.22 |
| array-loop-copy-checksum | 36864 | 3/0 | 3/0 | 27.41/27.41 | 18.33/18.28 |
| tree-survival | 19454688 | 147/1 | 147/1 | 27.48/27.48 | 31.04/30.82 |

Both old semispaces remain 8 MiB each; total young budget remains 2 MiB. Age2 is OFF; active nursery is 2 MiB.

## Compile footprint

Separate Compile+Close measurement, 256 iterations per process, three alternating fresh processes. Timing is noisy and is not peak compilation memory.

| Case | Native code B baseline/candidate | Go B/op baseline/candidate | Allocs/op baseline/candidate | Compile ns/op baseline/candidate |
|---|---|---|---|---|
| struct-allocation-access | 3183/3183 | 68131/68131 | 253/253 | 302344/251554 |
| array-allocation-access | 3439/3439 | 71987/71987 | 242/242 | 224247/200808 |
| array-fill | 3951/3951 | 76300/76300 | 246/246 | 233066/238508 |
| array-copy-checksum | 7791/7983 | 84260/83492 | 288/288 | 358582/286077 |
| array-loop-copy-checksum | 7791/7983 | 84260/83492 | 288/288 | 321537/279708 |
| tree-survival | 4015/4015 | 69731/69731 | 289/289 | 250911/220096 |

## Interpretation limits

All workers were idle during measurements, but this was not an exclusive physical host and no CPU affinity was introduced. Controls and process variation remain material. V8 internal allocation counts and optimizer elision are not observable here; these are end-to-end engine comparisons, not isolated collector-efficiency proofs. Keep every outlier and miss. Same code size is not proof of byte-identical emitted code. No historical cross-host timings are comparators.

SIMD changes only the weighted reduction path in the copy modules. Those modules grow 192 native bytes; other code sizes and compile allocation counts are unchanged. Full bulk-copy median improves while scalar-copy median regresses, and unchanged controls vary considerably. This run does not establish a reliable SIMD gain; do not label the bulk change a collector improvement.
