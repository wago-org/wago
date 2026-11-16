# age2-queue-reuse measured comparison

Qualified runtime: `b92d0e24f03e032c30700cea25badfd60b5c48fe`. Driver/probe-only commit is recorded in raw provenance. Same Intel Xeon Platinum 8573C host; Go 1.27.1; Node 24.22.1 / V8 13.6.233.17-node.58, `--no-liftoff`. No diagnostic tags. Both variants use identical source and the protocol tags documented in README.

All 12 actual-GC audits, 36 bounded processes, and 54 full processes passed SHA, checksum and exact Wago allocation checks. Full protocol: 100 warmup calls, nine batches of 32 calls, three rotating fresh-process rounds. Audits use 128 warmups. Bounded results use three warmups and five calls and are not pooled with full results.

**Every candidate case misses the 1.15x V8 target.** No default adoption or universal gain is justified. Values below are medians of the three process medians, in milliseconds.

| Case | Baseline | Candidate | V8 | Candidate/V8 | Candidate change |
|---|---:|---:|---:|---:|---:|
| struct-allocation-access | 9.4151 | 9.4031 | 1.2818 | 7.336 | -0.1% |
| array-allocation-access | 28.2718 | 26.5456 | 7.9891 | 3.323 | -6.1% |
| array-fill | 30.4426 | 29.6141 | 10.6015 | 2.793 | -2.7% |
| array-copy-checksum | 0.3475 | 0.3420 | 0.1577 | 2.168 | -1.6% |
| array-loop-copy-checksum | 0.3893 | 0.3456 | 0.1760 | 1.963 | -11.2% |
| tree-survival | 2.6061 | 2.5666 | 0.8681 | 2.956 | -1.5% |

## Per-round changes and bounded controls

| Case | Full changes, rounds 0/1/2 | Bounded changes, rounds 0/1/2 |
|---|---|---|
| struct-allocation-access | -10.4%, +2.0%, -0.1% | -16.8%, +0.6%, -18.2% |
| array-allocation-access | -27.7%, +5.5%, -3.8% | -27.0%, -23.1%, -1.3% |
| array-fill | +3.8%, -22.5%, -2.7% | +22.3%, +40.0%, +46.1% |
| array-copy-checksum | -1.5%, +6.2%, -11.2% | +67.4%, -34.5%, -25.9% |
| array-loop-copy-checksum | +20.8%, -26.3%, -11.2% | -1.3%, -4.1%, +55.4% |
| tree-survival | -5.1%, +9.6%, +1.5% | -9.0%, +18.5%, +30.6% |

## Allocation, collection and memory

Timing counters are real; copied/promoted and scan counters in generic Stats remain uninstrumented, so their zeros are not evidence of zero work. Each timed process makes 288 calls. No elapsed-time comparisons use diagnostic binaries.

| Case | Timed allocations | Baseline minor/full | Candidate minor/full | Go heap MiB baseline/candidate | VmHWM MiB baseline/candidate |
|---|---:|---|---|---|---|
| struct-allocation-access | 75515904 | 1536/0 | 1536/0 | 27.60/27.60 | 19.12/18.94 |
| array-allocation-access | 75515904 | 9216/0 | 9216/0 | 28.54/28.54 | 19.82/19.92 |
| array-fill | 75515904 | 9216/0 | 9216/0 | 28.54/28.54 | 19.95/19.88 |
| array-copy-checksum | 36864 | 5/0 | 5/0 | 27.41/27.41 | 18.34/18.37 |
| array-loop-copy-checksum | 36864 | 5/0 | 5/0 | 27.41/27.41 | 18.41/18.32 |
| tree-survival | 19454688 | 306/0 | 306/0 | 27.47/27.47 | 19.39/19.34 |

Both old semispaces remain 8 MiB each; total young budget remains 2 MiB. Age2 is ON in both: active/inactive halves 1 MiB each, survivor cap 512 KiB. Oversized threshold, geometry and policy are unchanged.

## Compile footprint

Separate Compile+Close measurement, 256 iterations per process, three alternating fresh processes. Timing is noisy and is not peak compilation memory.

| Case | Native code B baseline/candidate | Go B/op baseline/candidate | Allocs/op baseline/candidate | Compile ns/op baseline/candidate |
|---|---|---|---|---|
| struct-allocation-access | 3183/3183 | 68131/68131 | 253/253 | 259001/279611 |
| array-allocation-access | 3439/3439 | 71987/71987 | 242/242 | 226018/213586 |
| array-fill | 3951/3951 | 76300/76299 | 246/246 | 231247/234285 |
| array-copy-checksum | 7727/7727 | 82852/82852 | 289/289 | 333960/292146 |
| array-loop-copy-checksum | 7727/7727 | 82852/82852 | 289/289 | 327960/333468 |
| tree-survival | 4015/4015 | 69731/69731 | 289/289 | 230582/262174 |

## Interpretation limits

All workers were idle during measurements, but this was not an exclusive physical host and no CPU affinity was introduced. Controls and process variation remain material. V8 internal allocation counts and optimizer elision are not observable here; these are end-to-end engine comparisons, not isolated collector-efficiency proofs. Keep every outlier and miss. Same code size is not proof of byte-identical emitted code. No historical cross-host timings are comparators.

Queue reuse has unchanged emitted code sizes and compile allocation counts. Full medians are modestly lower, but bounded and per-round results are inconsistent, including large control reversals. This evidence does not yet establish a robust broad speedup. Collection counts are identical; the change does not alter the measured policy or geometry.
