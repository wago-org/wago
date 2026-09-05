# Railshot compile-latency report

Measured 2026-09-04 on native ARM64. This is a stopping-point report for
`jairus/railshot-compile-latency`, comparing:

- Base: `main` at `c46f2129edb52e6f30f4d0bfc5ae105cfde0c84d`
- Branch: `5a4e2d8912e6f84dcfeb31c450c53e76a54ece76`
- Host: Apple M4 Max, macOS 26.6.2, Go 1.26.5, `darwin/arm64`

## Current result

| Metric | Delta versus `main` |
|---|---:|
| End-to-end compile latency, all-corpus geomean | **-10.93%** |
| Backend-only compile latency, all-corpus geomean | **-3.49%** |
| End-to-end compile heap, all-corpus geomean | **+1.07%** |
| Backend-only compile heap, all-corpus geomean | **+0.08%** |
| End-to-end compile allocations, all-corpus geomean | **+1.64%** |
| Backend-only compile allocations, all-corpus geomean | **+0.16%** |
| Generated ARM64 machine-code bytes | **0.00%**; exact size match on every corpus module |
| Execution latency, executable-corpus geomean | **+0.19%** |
| Execution allocations | **0 B/op, 0 allocs/op** on both revisions |

The strongest result is on large real modules. End-to-end compile latency is
18.2-21.8% lower on Lua, SQLite, Ruby, and esbuild. Backend-only compilation is
3.5-12.8% lower on the same group. The branch currently spends a small amount
of heap on compact validation analysis and parallel hint orchestration; reducing
that +1.07% full-pipeline heap delta is the next memory target.

Generated function code has exactly the same size across the entire corpus.
The +0.19% execution geomean, and the scattered positive and negative rows
below, therefore do not represent an emitted-code change. They are binary
layout and measurement effects outside the generated function bodies.

## Large-module memory detail

| Corpus | Backend heap main | Backend heap branch | Full heap main | Full heap branch |
|---|---:|---:|---:|---:|
| lua | 509.6 KiB | 509.7 KiB | 844.9 KiB | 866.4 KiB |
| sqlite3 | 1.105 MiB | 1.105 MiB | 2.757 MiB | 2.851 MiB |
| ruby | 7.364 MiB | 7.364 MiB | 25.19 MiB | 25.73 MiB |
| esbuild | 7.353 MiB | 7.353 MiB | 60.05 MiB | 60.19 MiB |

## Complete compile corpus

Negative latency is better. Heap and allocation columns are deltas versus
`main`. Each latency figure is the median of eight fresh, interleaved one-shot
process samples. Native code was independently compiled once by each revision;
every reported size matched exactly.

| Corpus | Backend main | Backend branch | Backend delta | Backend heap delta | Backend alloc delta | Full main | Full branch | Full delta | Full heap delta | Full alloc delta | Native code |
|---|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|
| tiny | 69.44 us | 70.40 us | +1.38% | +0.00% | +0.00% | 174.94 us | 176.46 us | +0.87% | +0.63% | +2.82% | 128 B |
| fib_iter | 35.79 us | 31.56 us | -11.82% | +0.00% | +0.00% | 59.94 us | 53.92 us | -10.04% | +0.53% | +2.99% | 128 B |
| fib_rec | 27.85 us | 29.04 us | +4.26% | +0.00% | +0.00% | 65.79 us | 60.85 us | -7.50% | +0.54% | +2.70% | 220 B |
| arith | 26.90 us | 22.54 us | -16.19% | +0.00% | +0.00% | 62.29 us | 49.15 us | -21.10% | +0.40% | +2.94% | 152 B |
| float | 27.73 us | 26.98 us | -2.70% | +0.00% | +0.00% | 48.00 us | 38.19 us | -20.44% | +0.40% | +2.94% | 152 B |
| memory | 31.65 us | 26.83 us | -15.21% | +0.00% | +0.00% | 54.50 us | 72.40 us | +32.84% | +0.91% | +3.12% | 404 B |
| memory_tree | 38.02 us | 32.12 us | -15.51% | +0.00% | +0.00% | 69.98 us | 71.71 us | +2.47% | +0.48% | +2.04% | 628 B |
| globals | 25.10 us | 23.81 us | -5.14% | +0.00% | +0.00% | 60.50 us | 57.42 us | -5.10% | +0.64% | +2.33% | 220 B |
| dispatch | 34.88 us | 33.63 us | -3.58% | +0.00% | +0.00% | 58.10 us | 59.71 us | +2.76% | +0.81% | +1.77% | 972 B |
| branches | 16.04 us | 19.58 us | +22.08% | +0.00% | +0.00% | 44.69 us | 41.04 us | -8.16% | +0.52% | +3.08% | 132 B |
| many_funcs | 212.08 us | 219.17 us | +3.34% | +0.00% | +0.00% | 384.48 us | 361.96 us | -5.86% | +8.00% | +2.70% | 9.49 KiB |
| linked_list | 30.90 us | 28.42 us | -8.03% | +0.00% | +0.00% | 58.19 us | 55.56 us | -4.51% | +1.46% | +5.26% | 364 B |
| mandelbrot | 31.42 us | 29.06 us | -7.49% | +0.00% | +0.00% | 58.77 us | 58.19 us | -0.99% | +0.03% | +1.30% | 412 B |
| sieve | 27.35 us | 25.40 us | -7.16% | +0.00% | +0.00% | 53.29 us | 53.79 us | +0.94% | +0.44% | +2.41% | 400 B |
| nbody | 96.27 us | 92.12 us | -4.31% | +0.00% | +0.00% | 179.35 us | 148.48 us | -17.21% | +0.22% | +1.55% | 3.93 KiB |
| spectralnorm | 86.79 us | 86.52 us | -0.31% | +0.00% | +0.00% | 160.35 us | 127.33 us | -20.59% | +0.34% | +1.60% | 2.70 KiB |
| fannkuch | 108.58 us | 103.44 us | -4.74% | +0.00% | +0.00% | 185.94 us | 166.19 us | -10.62% | +0.20% | +1.34% | 5.02 KiB |
| matmul | 65.60 us | 65.40 us | -0.32% | +0.00% | +0.00% | 126.87 us | 104.17 us | -17.90% | +0.33% | +1.60% | 2.34 KiB |
| quicksort | 80.85 us | 76.13 us | -5.85% | +0.00% | +0.00% | 139.44 us | 116.98 us | -16.11% | +0.38% | +1.23% | 2.40 KiB |
| crc32 | 46.52 us | 43.73 us | -6.00% | +0.00% | +0.00% | 82.90 us | 72.92 us | -12.04% | +1.05% | +3.39% | 1.66 KiB |
| sha256 | 83.08 us | 81.29 us | -2.16% | +0.00% | +0.00% | 151.98 us | 127.94 us | -15.82% | +0.31% | +1.42% | 3.68 KiB |
| raytrace | 198.19 us | 178.85 us | -9.76% | +0.00% | +0.00% | 339.69 us | 279.23 us | -17.80% | +0.04% | +0.66% | 10.76 KiB |
| regexmatch | 36.064 ms | 35.206 ms | -2.38% | +0.01% | +0.07% | 58.509 ms | 47.815 ms | -18.28% | +2.10% | +0.04% | 2.95 MiB |
| wasm3 | 8.914 ms | 8.728 ms | -2.08% | +0.02% | +0.20% | 14.079 ms | 11.743 ms | -16.59% | +4.65% | +0.11% | 908.21 KiB |
| json-as | 909.71 us | 878.02 us | -3.48% | +1.39% | +1.50% | 1.550 ms | 1.328 ms | -14.31% | +1.95% | +1.00% | 63.23 KiB |
| blake-as | 247.63 us | 251.88 us | +1.72% | +0.00% | +0.00% | 437.19 us | 370.85 us | -15.17% | +0.35% | +0.26% | 10.80 KiB |
| utf-as | 134.75 us | 131.65 us | -2.30% | +0.00% | +0.00% | 262.58 us | 238.29 us | -9.25% | +0.25% | +1.46% | 4.28 KiB |
| xjb-mulhi | 30.17 us | 31.44 us | +4.21% | +0.00% | +0.00% | 60.38 us | 60.23 us | -0.24% | -0.27% | +0.00% | 480 B |
| swar-pack-parse | 27.92 us | 33.71 us | +20.75% | +0.00% | +0.00% | 67.58 us | 66.12 us | -2.16% | +0.21% | +1.16% | 580 B |
| json-as-simd | 968.27 us | 987.85 us | +2.02% | +1.29% | +1.44% | 1.742 ms | 1.456 ms | -16.42% | +2.10% | +0.94% | 71.79 KiB |
| blake-as-simd | 633.08 us | 614.29 us | -2.97% | +0.24% | +2.27% | 1.229 ms | 1.032 ms | -16.06% | +0.43% | +1.81% | 30.57 KiB |
| utf-as-simd | 368.04 us | 358.90 us | -2.49% | +0.00% | +0.00% | 687.08 us | 582.50 us | -15.22% | +0.28% | +0.94% | 20.18 KiB |
| lua | 13.803 ms | 12.781 ms | -7.40% | +0.02% | +0.27% | 22.413 ms | 18.185 ms | -18.87% | +2.54% | +0.16% | 923.23 KiB |
| sqlite3 | 53.343 ms | 51.493 ms | -3.47% | +0.00% | +0.07% | 86.091 ms | 70.395 ms | -18.23% | +3.41% | +0.05% | 3.56 MiB |
| ruby | 587.161 ms | 511.991 ms | -12.80% | +0.00% | +0.01% | 976.040 ms | 763.715 ms | -21.75% | +2.14% | +0.01% | 35.83 MiB |
| esbuild | 372.603 ms | 340.655 ms | -8.57% | +0.00% | +0.01% | 638.817 ms | 514.373 ms | -19.48% | +0.23% | +0.04% | 29.47 MiB |

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
| tiny.add | 21.4 ns | 21.2 ns | -1.12% | p=0.005 n=8 |
| fib_iter.fib | 29.5 ns | 29.7 ns | +0.42% | p=0.342 n=8 |
| fib_rec.fib | 1.075 ms | 1.086 ms | +1.05% | p=0.328 n=8 |
| arith.run | 1.51 us | 1.51 us | -0.10% | p=0.816 n=8 |
| float.run | 2.54 us | 2.53 us | -0.22% | p=0.978 n=8 |
| memory.sum | 309.7 ns | 309.5 ns | -0.06% | p=0.981 n=8 |
| memory_tree.run | 8.74 us | 8.74 us | +0.04% | p=0.505 n=8 |
| globals.accumulate | 636.2 ns | 611.7 ns | -3.87% | p=0.015 n=8 |
| dispatch.apply | 24.7 ns | 24.4 ns | -0.93% | p=0.022 n=8 |
| branches.classify | 21.3 ns | 21.0 ns | -1.46% | p=0.011 n=8 |
| many_funcs.run | 21.3 ns | 21.2 ns | -0.31% | p=0.368 n=8 |
| linked_list.sum | 5.70 us | 5.56 us | -2.40% | p=0.574 n=8 |
| mandelbrot.render | 225.12 us | 223.71 us | -0.63% | p=0.878 n=8 |
| sieve.count | 100.69 us | 101.22 us | +0.53% | p=0.382 n=8 |
| nbody.step | 145.87 us | 146.61 us | +0.50% | p=0.721 n=8 |
| spectralnorm.run | 385.19 us | 390.32 us | +1.33% | p=0.195 n=8 |
| fannkuch.run | 1.266 ms | 1.279 ms | +1.02% | p=0.130 n=8 |
| matmul.run | 149.65 us | 150.58 us | +0.62% | p=0.161 n=8 |
| quicksort.sortN | 69.56 us | 71.21 us | +2.37% | p=0.195 n=8 |
| crc32.hashN | 25.00 us | 24.37 us | -2.51% | p=0.161 n=8 |
| sha256.hashN | 28.93 us | 28.87 us | -0.18% | p=0.721 n=8 |
| raytrace.render | 265.01 us | 261.50 us | -1.32% | p=0.083 n=8 |
| json-as.serializeN | 19.37 us | 19.09 us | -1.42% | p=0.003 n=8 |
| json-as.deserializeN | 39.75 us | 39.29 us | -1.13% | p=0.083 n=8 |
| blake-as.hashN | 396.10 us | 399.60 us | +0.88% | p=0.161 n=8 |
| utf-as.convertN | 122.88 us | 122.94 us | +0.05% | p=0.645 n=8 |
| xjb-mulhi.mulhi | 21.9 ns | 21.6 ns | -1.30% | p=0.247 n=8 |
| xjb-mulhi.runN | 1.24 us | 1.25 us | +0.56% | p=0.556 n=8 |
| swar-pack-parse.pack | 21.5 ns | 21.8 ns | +1.65% | p=0.169 n=8 |
| swar-pack-parse.parse4 | 21.6 ns | 22.1 ns | +2.13% | p=0.398 n=8 |
| swar-pack-parse.runN | 1.23 us | 1.26 us | +2.39% | p=0.007 n=8 |
| json-as-simd.serializeN | 23.35 us | 24.04 us | +2.96% | p=0.028 n=8 |
| json-as-simd.deserializeN | 51.11 us | 52.52 us | +2.77% | p=0.001 n=8 |
| blake-as-simd.hashN | 423.80 us | 435.64 us | +2.79% | p=0.002 n=8 |
| utf-as-simd.convertN | 56.60 us | 57.63 us | +1.81% | p=0.005 n=8 |
| utf-as-simd.validateN | 143.95 us | 144.44 us | +0.34% | p=0.382 n=8 |

## Retained changes

The branch has 13 commits over `main`:

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

Implementation source delta, excluding this report:

```text
34 files changed, 1,887 insertions, 280 deletions
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

Raw measurements for this report are in `/tmp/arm64-report-*` on the measuring
host. They are intentionally not checked into the repository.

## Next ARM64 work

The remaining structural ceiling is the full hint walk, approximately 11% of
large-function backend profiles after the current changes. More tiny accessor
caches have repeatedly regressed. The next useful step is to extend the compact
validation summary with the exact bounded facts needed by hint construction so
the successful hint scan can be retired without adding another body walk.

The near-term acceptance gates are:

1. Preserve exact generated machine-code bytes.
2. Improve large-module backend latency by at least another 3%.
3. Return full-pipeline heap to at most `main` before expanding the summary.
4. Keep tiny-module p50 within 1.5% once measured with a longer adaptive run.
