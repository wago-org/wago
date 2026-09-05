# Railshot compile-latency report

Measured 2026-09-04 on native ARM64. This is a stopping-point report for
`jairus/railshot-compile-latency`, comparing:

- Base: `main` at `c46f2129edb52e6f30f4d0bfc5ae105cfde0c84d`
- Branch: `2df5838cef1349eda176200e93b09c6ae93bc02b`
- Host: Apple M4 Max, macOS 26.6.2, Go 1.26.5, `darwin/arm64`

## Current result

| Metric | Delta versus `main` |
|---|---:|
| End-to-end compile latency, all-corpus geomean | **-9.38%** |
| End-to-end compile latency, large-module geomean | **-22.22%** |
| Backend-only compile latency, large-module geomean | **-10.33%** |
| Backend-only compile latency, all-corpus one-shot geomean | **+0.86%** |
| End-to-end compile heap, all-corpus geomean | **+1.07%** |
| Backend-only compile heap, all-corpus geomean | **+0.08%** |
| End-to-end compile allocations, all-corpus geomean | **+1.66%** |
| Backend-only compile allocations, all-corpus geomean | **+0.16%** |
| Generated ARM64 machine-code bytes | **0.00%**; exact size match on every corpus module |
| Execution latency, executable-corpus geomean | **+0.23%** |
| Execution allocations | **0 B/op, 0 allocs/op** on both revisions |

The complete aggregate above was measured at implementation commit `e5b2431a`.
Current HEAD adds one code-neutral ARM64 scanner change measured separately
against that checkpoint: **-0.54% backend compile-latency geomean** across
json-as, Lua, SQLite, Ruby, and esbuild, with all five medians improving. The
focused sparse-global hint scan improved from a 100.0 us median to 96.2 us
(**-3.8%**), while the sparse-local scan and allocation counts were flat. A
fresh complete-corpus machine-code check at current HEAD matched every module
and worker setting exactly. Execution was therefore not rerun for this
compiler-only dispatch change.

The strongest and most stable result is on large real modules. End-to-end
compile latency is 19.6-24.8% lower on Lua, SQLite, Ruby, and esbuild.
Backend-only compilation is 3.5-11.9% lower on the same group. The all-corpus
backend geomean includes fresh-process micro modules whose 7-59% confidence
intervals overwhelm their tens-of-microseconds signal; it is reported rather
than filtered, but is not evidence of a backend regression. The branch
currently spends a small amount of heap on compact validation analysis and
parallel hint orchestration; reducing that +1.07% full-pipeline heap delta is
the next memory target.

Generated function code has exactly the same size across the entire corpus,
and neither retained change is intended to alter lowering. The +0.23%
execution geomean and scattered positive and negative rows are consistent with
binary-layout and measurement noise; no broad execution shift is visible.

## Large-module memory detail

| Corpus | Backend heap main | Backend heap branch | Full heap main | Full heap branch |
|---|---:|---:|---:|---:|
| lua | 509.6 KiB | 509.7 KiB | 844.9 KiB | 866.4 KiB |
| sqlite3 | 1.105 MiB | 1.105 MiB | 2.757 MiB | 2.851 MiB |
| ruby | 7.364 MiB | 7.364 MiB | 25.19 MiB | 25.73 MiB |
| esbuild | 7.353 MiB | 7.353 MiB | 60.05 MiB | 60.18 MiB |

## Complete compile corpus

Negative latency is better. Heap and allocation columns are deltas versus
`main`. Each latency figure is the median of eight fresh, interleaved one-shot
process samples. Native code was independently compiled once by each revision;
every reported size matched exactly.

| Corpus | Backend main | Backend branch | Backend delta | Backend heap delta | Backend alloc delta | Full main | Full branch | Full delta | Full heap delta | Full alloc delta | Native code |
|---|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|
| tiny | 67.65 us | 72.27 us | +6.84% | +0.00% | +0.00% | 171.81 us | 182.88 us | +6.44% | +0.63% | +2.82% | 128 B |
| fib_iter | 29.23 us | 30.56 us | +4.56% | +0.00% | +0.00% | 57.50 us | 54.02 us | -6.05% | +0.53% | +2.99% | 128 B |
| fib_rec | 26.67 us | 34.50 us | +29.38% | +0.00% | +0.00% | 54.56 us | 66.77 us | +22.37% | +0.54% | +2.70% | 220 B |
| arith | 24.04 us | 25.31 us | +5.29% | +0.00% | +0.00% | 51.06 us | 54.33 us | +6.40% | +0.40% | +2.94% | 152 B |
| float | 23.40 us | 26.96 us | +15.23% | +0.00% | +0.00% | 46.88 us | 48.23 us | +2.89% | +0.40% | +2.94% | 152 B |
| memory | 28.04 us | 28.33 us | +1.04% | +0.00% | +0.00% | 56.27 us | 65.46 us | +16.33% | +0.65% | +2.47% | 404 B |
| memory_tree | 30.71 us | 36.10 us | +17.57% | +0.00% | +0.00% | 74.17 us | 67.83 us | -8.54% | +0.48% | +2.04% | 628 B |
| globals | 23.08 us | 21.52 us | -6.77% | +0.00% | +0.00% | 60.44 us | 57.15 us | -5.45% | +0.64% | +2.33% | 220 B |
| dispatch | 33.25 us | 32.52 us | -2.19% | +0.00% | +0.00% | 65.21 us | 61.94 us | -5.02% | +0.81% | +1.77% | 972 B |
| branches | 16.96 us | 17.62 us | +3.93% | +0.00% | +0.00% | 42.92 us | 43.44 us | +1.21% | +0.52% | +3.08% | 132 B |
| many_funcs | 218.15 us | 211.69 us | -2.96% | +0.00% | +0.00% | 373.88 us | 352.04 us | -5.84% | +8.00% | +2.70% | 9.49 KiB |
| linked_list | 25.79 us | 26.54 us | +2.91% | +0.00% | +0.00% | 51.13 us | 50.65 us | -0.94% | +0.48% | +2.56% | 364 B |
| mandelbrot | 27.25 us | 30.08 us | +10.40% | +0.00% | +0.00% | 57.92 us | 57.75 us | -0.29% | +0.38% | +2.60% | 412 B |
| sieve | 23.62 us | 25.44 us | +7.67% | +0.00% | +0.00% | 50.94 us | 52.33 us | +2.74% | +0.44% | +2.41% | 400 B |
| nbody | 94.58 us | 90.96 us | -3.83% | +0.00% | +0.00% | 170.73 us | 139.02 us | -18.57% | +0.44% | +2.34% | 3.93 KiB |
| spectralnorm | 83.17 us | 86.52 us | +4.03% | +0.00% | +0.00% | 147.02 us | 133.83 us | -8.97% | +0.68% | +2.42% | 2.70 KiB |
| fannkuch | 106.67 us | 106.13 us | -0.51% | +0.00% | +0.00% | 195.56 us | 158.79 us | -18.80% | +0.20% | +1.34% | 5.02 KiB |
| matmul | 69.44 us | 60.46 us | -12.93% | +0.00% | +0.00% | 124.60 us | 110.17 us | -11.59% | +0.33% | +1.60% | 2.34 KiB |
| quicksort | 77.31 us | 71.44 us | -7.60% | +0.00% | +0.00% | 134.19 us | 120.67 us | -10.08% | +0.38% | +1.23% | 2.40 KiB |
| crc32 | 39.75 us | 45.02 us | +13.26% | +0.00% | +0.00% | 82.94 us | 78.98 us | -4.77% | +0.00% | +0.83% | 1.66 KiB |
| sha256 | 75.25 us | 80.79 us | +7.36% | +0.00% | +0.00% | 148.92 us | 125.10 us | -15.99% | +0.31% | +1.42% | 3.68 KiB |
| raytrace | 187.17 us | 175.69 us | -6.13% | +0.00% | +0.00% | 342.60 us | 270.10 us | -21.16% | +0.17% | +1.32% | 10.76 KiB |
| regexmatch | 36.266 ms | 34.291 ms | -5.44% | +0.01% | +0.07% | 57.104 ms | 45.730 ms | -19.92% | +2.11% | +0.06% | 2.95 MiB |
| wasm3 | 9.029 ms | 8.711 ms | -3.52% | +0.02% | +0.20% | 13.468 ms | 11.112 ms | -17.49% | +4.65% | +0.11% | 908.21 KiB |
| json-as | 899.60 us | 853.27 us | -5.15% | +1.39% | +1.50% | 1.472 ms | 1.227 ms | -16.63% | +1.95% | +0.99% | 63.23 KiB |
| blake-as | 266.77 us | 246.13 us | -7.74% | +0.00% | +0.00% | 411.35 us | 346.00 us | -15.89% | +0.63% | +1.04% | 10.80 KiB |
| utf-as | 126.25 us | 117.17 us | -7.19% | +0.00% | +0.00% | 249.58 us | 226.65 us | -9.19% | +0.25% | +1.44% | 4.28 KiB |
| xjb-mulhi | 28.48 us | 28.50 us | +0.07% | +0.00% | +0.00% | 53.73 us | 54.23 us | +0.93% | +0.11% | +1.14% | 480 B |
| swar-pack-parse | 26.63 us | 33.50 us | +25.82% | +0.00% | +0.00% | 57.23 us | 57.06 us | -0.29% | +0.59% | +2.33% | 580 B |
| json-as-simd | 974.60 us | 986.69 us | +1.24% | +1.29% | +1.44% | 1.680 ms | 1.352 ms | -19.51% | +2.10% | +0.94% | 71.79 KiB |
| blake-as-simd | 656.71 us | 618.58 us | -5.81% | +0.24% | +2.27% | 1.211 ms | 921.69 us | -23.89% | +0.43% | +1.81% | 30.57 KiB |
| utf-as-simd | 359.69 us | 350.00 us | -2.69% | +0.00% | +0.00% | 679.52 us | 534.25 us | -21.38% | +0.28% | +0.94% | 20.18 KiB |
| lua | 13.828 ms | 13.053 ms | -5.61% | +0.02% | +0.27% | 21.979 ms | 17.353 ms | -21.05% | +2.54% | +0.16% | 923.23 KiB |
| sqlite3 | 53.689 ms | 51.818 ms | -3.48% | +0.00% | +0.07% | 85.234 ms | 67.460 ms | -20.85% | +3.40% | +0.00% | 3.56 MiB |
| ruby | 591.209 ms | 521.049 ms | -11.87% | +0.00% | +0.01% | 964.440 ms | 725.167 ms | -24.81% | +2.14% | +0.01% | 35.83 MiB |
| esbuild | 374.843 ms | 342.821 ms | -8.54% | +0.00% | +0.01% | 627.529 ms | 504.845 ms | -19.55% | +0.22% | +0.00% | 29.47 MiB |

The one-shot micro rows have much wider confidence intervals than the real
modules. They are included for completeness, not treated as individual proof of
a regression or improvement. The geomeans and large-module rows are the useful
decision signals.

## Complete execution corpus

Each row is the median of eight interleaved 100 ms samples. Because generated
machine-code sizes are exactly identical and all retained compiler changes are
intended to preserve lowering decisions, small execution movements are treated
as layout/noise unless reproduced with instruction-byte hashes and controlled
binary layout.

| Workload | Main | Branch | Delta | Mann-Whitney result |
|---|---:|---:|---:|---:|
| tiny.add | 21.0 ns | 21.1 ns | +0.52% | p=0.524 n=8 |
| fib_iter.fib | 29.2 ns | 29.2 ns | +0.12% | p=0.574 n=8 |
| fib_rec.fib | 1.115 ms | 1.072 ms | -3.86% | p=0.442 n=8 |
| arith.run | 1.48 us | 1.48 us | -0.10% | p=0.983 n=8 |
| float.run | 2.53 us | 2.57 us | +1.52% | p=0.442 n=8 |
| memory.sum | 307.9 ns | 311.3 ns | +1.12% | p=0.279 n=8 |
| memory_tree.run | 8.66 us | 8.68 us | +0.18% | p=0.505 n=8 |
| globals.accumulate | 598.6 ns | 631.2 ns | +5.44% | p=0.169 n=8 |
| dispatch.apply | 24.2 ns | 24.3 ns | +0.77% | p=0.984 n=8 |
| branches.classify | 21.1 ns | 21.0 ns | -0.88% | p=0.523 n=8 |
| many_funcs.run | 21.4 ns | 21.2 ns | -0.84% | p=0.395 n=8 |
| linked_list.sum | 5.53 us | 5.64 us | +2.10% | p=0.195 n=8 |
| mandelbrot.render | 219.58 us | 218.56 us | -0.46% | p=0.721 n=8 |
| sieve.count | 100.19 us | 98.91 us | -1.28% | p=0.291 n=8 |
| nbody.step | 144.29 us | 143.64 us | -0.45% | p=0.959 n=8 |
| spectralnorm.run | 388.24 us | 383.33 us | -1.26% | p=0.328 n=8 |
| fannkuch.run | 1.256 ms | 1.252 ms | -0.37% | p=0.959 n=8 |
| matmul.run | 149.02 us | 148.06 us | -0.64% | p=0.721 n=8 |
| quicksort.sortN | 69.39 us | 69.12 us | -0.39% | p=0.721 n=8 |
| crc32.hashN | 23.40 us | 23.76 us | +1.57% | p=0.130 n=8 |
| sha256.hashN | 28.30 us | 28.38 us | +0.29% | p=0.798 n=8 |
| raytrace.render | 258.26 us | 257.33 us | -0.36% | p=0.574 n=8 |
| json-as.serializeN | 19.01 us | 18.97 us | -0.22% | p=0.878 n=8 |
| json-as.deserializeN | 39.35 us | 39.43 us | +0.19% | p=0.505 n=8 |
| blake-as.hashN | 396.99 us | 398.36 us | +0.34% | p=0.505 n=8 |
| utf-as.convertN | 123.31 us | 126.01 us | +2.19% | p=0.130 n=8 |
| xjb-mulhi.mulhi | 21.6 ns | 21.9 ns | +1.67% | p=0.700 n=8 |
| xjb-mulhi.runN | 1.23 us | 1.24 us | +0.73% | p=0.314 n=8 |
| swar-pack-parse.pack | 21.6 ns | 21.1 ns | -2.29% | p=0.574 n=8 |
| swar-pack-parse.parse4 | 22.1 ns | 22.2 ns | +0.72% | p=0.721 n=8 |
| swar-pack-parse.runN | 1.24 us | 1.25 us | +1.42% | p=0.031 n=8 |
| json-as-simd.serializeN | 23.58 us | 23.48 us | -0.42% | p=0.721 n=8 |
| json-as-simd.deserializeN | 51.22 us | 51.00 us | -0.43% | p=0.721 n=8 |
| blake-as-simd.hashN | 419.25 us | 422.88 us | +0.87% | p=0.505 n=8 |
| utf-as-simd.convertN | 56.01 us | 56.39 us | +0.68% | p=0.382 n=8 |
| utf-as-simd.validateN | 142.03 us | 142.49 us | +0.32% | p=0.645 n=8 |

## Retained changes

The branch has 18 commits over `main`, including two report checkpoints, and 16
retained implementation changes:

1. Faster ARM64 byte-backed hint decoding.
2. Separation of opt-in statistics from inline reports.
3. Compact validation-time function analysis collected in the existing walk.
4. Reuse of validation facts during compiler admission.
5. Reuse of validation call and GC facts.
6. Parallel ARM64 module hint scanning.
7. Parallel AMD64 module hint scanning.
8. Cached AMD64 module value types.
9. Cached ARM64 module value types.
10. Faster AMD64 function hint decoding.
11. Fused AMD64 inline caller analysis.
12. Fast AMD64 inline caller immediate decoding.
13. Hoisted ARM64 hint-path weighting.
14. Table-driven validation instruction facts.
15. Skipped irrelevant ARM64 hint-discount classification.
16. Direct ARM64 hint-boundary classification in the opcode dispatch.

Implementation source delta, excluding this report:

```text
35 files changed, 1,986 insertions, 300 deletions
```

The larger source increase is primarily validation-analysis structure, tests,
and duplicated architecture-specific integration. The generated code remains
unchanged.

## Rejected ARM64 probes

These were measured and removed rather than retained speculatively:

| Probe | Result |
|---|---|
| Unsafe 32-bit instruction store in `Asm.word` | Existing append lowering already emits one store; unsafe form added work |
| Saturated `uint32` local hotness | +1.25% focused geomean; esbuild +1.58% |
| Lookup table for loop weighting | Unstable and slower overall |
| Pending-memory-reference counter | Flat: 131.7 us scan versus 131.0 us counter, p=0.887 |
| Direct reader bounds check | +15.45% focused geomean; json-as +70.84% |
| Dense function-signature pointer cache | +13.47% focused geomean, +1.07% heap |
| Cached imported-function boundary | +5.32% focused geomean; json-as +26.93% |
| Heavy-first scheduling | -0.04%; below measurement value |
| Validation-to-hints fusion | Exact hint and code parity, but +0.20% full-compile latency geomean, +0.53% heap, and +1.33% esbuild latency (p=0.015, n=8); bounded per-function fallback and packed shared headers could not make the extra validation state pay for the removed scan |
| Bitmask stack-flow terminator classification | Short run looked positive; 500 ms x 8 confirmation reversed to +0.90% geomean and +1.28% SQLite (p=0.005) |
| Specialized scalar load/store encoder helpers | -0.43% in the first interleaved run, but no individual workload was significant; an unchecked scaled-encoding variant regressed by +0.74%, so the extra surface was removed |
| Cached memory-zero address width and module type lookup | Short samples were noisy; the expanded cache regressed the focused geomean by +0.98% and Ruby by +1.35% (p=0.028), so it was removed |

## Method

- Built separate test binaries from the exact base and branch revisions.
- Ran backend and public full-pipeline compilation in alternating base/branch
  order, eight fresh one-shot process samples per corpus.
- Ran execution in alternating base/branch order, eight 100 ms samples per
  exported workload.
- Compared distributions with `benchstat`.
- Used the benchmark allocator counters for `B/op` and `allocs/op`.
- Compiled every corpus module independently on both revisions and compared
  final native code size.
- Kept decode and validation outside `BenchmarkCompile`; included them in
  `BenchmarkCompileFull`.

Raw measurements for the current checkpoint are in
`/tmp/arm64-current-{main,branch}-{full-all,backend-all,exec}.txt` and the
corresponding `benchstat` and CSV files on the measuring host. They are
intentionally not checked into the repository.

## Next ARM64 work

With inlining disabled in the current large-module profile, scalar memory
lowering is the largest visible family: `fn.ldst` is 20.58% flat and
`memAddr` is 24.85% cumulative. The byte-backed hint scanner is 10.29%
cumulative, and immediate materialization remains visible at 5.83% flat.
Simple load/store helper specialization, unchecked scaled encoders, and module
memory-type caches did not clear the retention gate. A complete
validation-to-hints fusion was also implemented and rejected because it
increased latency and heap, so the next work should remain narrow and
profile-led rather than retaining more summary state.

The near-term acceptance gates are:

1. Preserve exact generated machine-code bytes.
2. Improve large-module backend latency by at least another 3%.
3. Return full-pipeline heap to at most `main` before expanding the summary.
4. Keep tiny-module p50 within 1.5% once measured with a longer adaptive run.
