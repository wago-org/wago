# Railshot compile-latency report

Measured through 2026-09-05 on native ARM64. This is a stopping-point report for
`jairus/railshot-compile-latency`, comparing:

- Base: `main` at `c46f2129edb52e6f30f4d0bfc5ae105cfde0c84d`
- Branch: `82ec3c14fde696e3a1d16a961b33fcda23ebf847`
- Host: Apple M4 Max, macOS 26.6.2, Go 1.26.5, `darwin/arm64`

## Current result

| Metric | Delta versus `main` |
|---|---:|
| End-to-end compile latency, all-corpus geomean | **-9.38%** |
| End-to-end compile latency, large-module geomean | **-24.29%** |
| Backend-only compile latency, large-module geomean | **-11.21%** |
| Backend-only compile latency, all-corpus one-shot geomean | **+0.86%** |
| End-to-end compile heap, large-module geomean | **+0.63%** |
| End-to-end compile heap, older all-corpus checkpoint | **+1.07%** |
| Backend-only compile heap, large-module geomean | **+0.17%** |
| End-to-end compile allocations, all-corpus geomean | **+1.66%** |
| Backend-only compile allocations, all-corpus geomean | **+0.16%** |
| Generated ARM64 machine-code bytes | **-1.37%** large-module geomean; up to **-3.2%** |
| Execution latency, complete-corpus geomean | **-1.65%** |
| Execution allocations | **0 B/op, 0 allocs/op** on both revisions |

The exact current-HEAD large-corpus comparison used eight fresh interleaved
300 ms samples per revision:

| Corpus | Full compile main | Full compile branch | Latency delta | Heap delta | Allocation delta |
|---|---:|---:|---:|---:|---:|
| json-as | 1.323 ms | 1.012 ms | **-23.50%** | +1.27% | +1.01% |
| Lua | 22.58 ms | 17.32 ms | **-23.30%** | +0.61% | +0.16% |
| SQLite | 86.16 ms | 66.00 ms | **-23.40%** | +0.79% | +0.03% |
| Ruby | 959.6 ms | 701.2 ms | **-26.93%** | +0.55% | statistically flat |
| esbuild | 627.7 ms | 475.4 ms | **-24.26%** | +0.01% | -0.06% |
| **Geomean** | **68.88 ms** | **52.15 ms** | **-24.29%** | **+0.63%** | **+0.23%** |

The matching backend-only geomean improved from 41.79 ms to 37.10 ms
(**-11.21%**). Every per-corpus backend and end-to-end latency result was
significant at `p<0.001`. Backend allocated heap was effectively flat at
**+0.17%** geomean; SQLite, Ruby, and esbuild each used less backend heap than
`main`.

The older all-corpus compile-latency and heap rows below were measured at
implementation commit `e5b2431a`. The exact current large-module comparison
above includes every retained change. The current branch also has these later
changes measured independently against their immediate predecessors:

| Incremental change | Backend compile | Full compile | Heap | Generated code / execution |
|---|---:|---:|---:|---|
| Direct hint-boundary dispatch | **-0.54%** | not rerun | unchanged | Exact code-byte parity |
| Reuse clean memory-address proofs | **-0.66%** | **-0.19%** | non-increasing | 0.5-3.2% less code on the large-module sample; selected memory-heavy execution rows improved |
| Dispatch integer/float constant hint work | **-0.18%** | not rerun | unchanged | Exact code-byte parity |
| Dispatch stack-flow termination locally | **-1.75%** | not rerun | unchanged | Exact code-byte parity |
| Compact validated segment counts | not rerun | **+0.30%** (flat) | **-0.45% full heap** | Exact code-byte parity |
| Narrow validation flags to 32 bits | not rerun | **+0.03%** (flat) | **-0.28% full heap** | Exact code-byte parity |
| Retain only consumed validation facts | not rerun | **-0.11%** (flat) | **-0.41% full heap** | Exact code-byte parity |
| Move segment counts to module analysis | not rerun | **+0.01%** (flat) | **-0.27% full heap** | Exact code-byte parity |
| Fuse validation fact observation | not rerun | **-1.35%** | unchanged | Exact code-byte parity |

The scanner result covers json-as, Lua, SQLite, Ruby, and esbuild, with all five
medians improving. Its focused sparse-global hint scan improved from a 100.0 us
median to 96.2 us (**-3.8%**), while the sparse-local scan and allocation counts
were flat. A complete compile-worker check matched every module and worker
setting exactly before the address-proof change.

The address-proof change deliberately removes redundant generated instructions.
Code fell by 352 B for json-as, 10,064 B for Lua, 31,232 B for SQLite, 524,128 B
for Ruby, and 1,002,148 B for esbuild. Focused execution medians improved by
1.4% for memory-tree, 3.8% for linked-list, 4.4% for matmul, 11.0% for CRC32,
and 4.1% for SHA-256. A longer Mandelbrot confirmation was flat (+0.4%). The
full semantic suite and an explicit non-canonical-upper-address regression passed.

The two newest scanner changes remove common-path classification while retaining
the same hint decisions. Dispatch-local constant handling improved the focused
global/local hint scans by about 1.0%/1.9%. Dispatch-local stack-flow termination
improved them by 3.1%/1.2% and improved the five-corpus backend geomean by 1.75%:
JSON -2.7%, SQLite -2.4%, Ruby -3.0%, and esbuild -1.6%, with Lua +1.1% in that
sample. Allocations and generated code were unchanged, and exhaustive tests now
cover every stack-flow terminator encoding.

The validation summary stores segment counts once per module analysis rather
than once per validation worker. Flags use their required 32 bits, and
validation no longer records cost, local-count, or depth fields that have no
production consumer. Together these changes shrink `ValidatedFuncFacts` from
32 to 8 bytes. The latest fused observer replaces two common per-op non-inline
calls with one direct observation path; a longer five-corpus run improved full
compile latency by 1.35%, with heap and allocation counts unchanged. Generated
code matched exactly for both latest changes.

The strongest and most stable result is on large real modules. End-to-end
compile latency is 23.3-26.9% lower on json-as, Lua, SQLite, Ruby, and esbuild.
Backend-only compilation is 7.7-16.6% lower on the same group. The all-corpus
backend geomean includes fresh-process micro modules whose 7-59% confidence
intervals overwhelm their tens-of-microseconds signal; it is reported rather
than filtered, but is not evidence of a backend regression. The branch
currently spends a small amount of heap on compact validation analysis and
parallel hint orchestration. At current HEAD, a fresh eight-pair comparison of
json-as, Lua, SQLite, Ruby, and esbuild is 24.29% faster end to end than pinned
`main`, with 0.63% more allocated heap and 0.23% more allocations. Every latency
result is significant at p<0.001. The remaining heap delta is concentrated in
compact validation analysis and parallel orchestration; esbuild is within 0.01%
of `main`.

Before the address-proof change, generated function code had exactly the same
size across the entire corpus. The address-proof change intentionally removes
redundant instructions; its focused execution measurements found improvements
on memory-heavy rows and no confirmed regression. The older complete execution
table below remains the representation-only checkpoint at `e5b2431a`, not a
claim about exact instruction parity at current HEAD.

## Current generated code and execution

The only retained change that intentionally alters generated code is reuse of
already-proven-clean memory32 addresses. The two newest validation changes are
byte-identical to their predecessors. Against pinned `main`, current generated
ARM64 code is 1.37% smaller by geomean across the five large modules:

| Corpus | Main code | Branch code | Delta |
|---|---:|---:|---:|
| json-as | 76,472 B | 76,104 B | -0.48% |
| Lua | 1,000,820 B | 990,852 B | -1.00% |
| SQLite | 3,957,880 B | 3,926,360 B | -0.80% |
| Ruby | 39,294,784 B | 38,769,328 B | -1.34% |
| esbuild | 31,288,340 B | 30,286,192 B | -3.20% |

A fresh complete execution-corpus run used eight interleaved 100 ms samples per
revision. The geomean improved **1.65%**, with **0 B/op and 0 allocs/op** on both
revisions. Significant improvements included memory sum -3.66%, memory tree
-3.18%, nbody -2.35%, matmul -4.23%, CRC32 -12.48%, SHA-256 -6.45%, json-as
deserialize -4.17%, json-as SIMD serialize -3.61%, BLAKE SIMD -4.36%, and UTF
SIMD convert -5.12%/validate -2.15%. No row showed a statistically significant
regression.

## Large-module memory detail

| Corpus | Backend heap main | Backend heap branch | Full heap main | Full heap branch |
|---|---:|---:|---:|---:|
| json-as | 51.82 KiB | 52.53 KiB | 108.8 KiB | 110.1 KiB |
| lua | 509.5 KiB | 509.6 KiB | 842.6 KiB | 847.8 KiB |
| sqlite3 | 1.105 MiB | 1.104 MiB | 2.755 MiB | 2.777 MiB |
| ruby | 7.364 MiB | 7.362 MiB | 25.19 MiB | 25.33 MiB |
| esbuild | 7.353 MiB | 7.323 MiB | 60.05 MiB | 60.05 MiB |

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

The branch has 32 commits over `main`, including eight report checkpoints, and
24 retained implementation changes:

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
17. Reused proven-clean memory32 addresses without redundant truncation.
18. Dispatch-local ARM64 constant hint work.
19. Dispatch-local ARM64 stack-flow termination tracking.
20. Compact validation segment counts with exact overflow fallback.
21. Narrowed validation flags to their required 32 bits.
22. Removed validation aggregates with no production consumer, leaving a
    12-byte per-function record.
23. Moved exact segment counts out of every worker/function record and into the
    module analysis, leaving an 8-byte per-function record.
24. Fused common validation fact observation into one direct per-op path.

Implementation source delta, excluding this report:

```text
36 files changed, 2,171 insertions, 337 deletions
```

The larger source increase is primarily validation-analysis structure, tests,
and duplicated architecture-specific integration. Only the clean-address
change intentionally alters generated code.

## Rejected ARM64 probes

These were measured and removed rather than retained speculatively:

| Probe | Result |
|---|---|
| Unsafe 32-bit instruction store in `Asm.word` | Existing append lowering already emits one store; unsafe form added work |
| Saturated `uint32` local hotness | +1.25% focused geomean; esbuild +1.58% |
| Lookup table for loop weighting | Unstable and slower overall |
| Pending-memory-reference counter | +0.44% backend geomean; Ruby +3.35% (p=0.038); no heap benefit |
| Direct reader bounds check | +15.45% focused geomean; json-as +70.84% |
| Dense function-signature pointer cache | +13.47% focused geomean, +1.07% heap |
| Cached imported-function boundary | +5.32% focused geomean; json-as +26.93% |
| Heavy-first scheduling | -0.04%; below measurement value |
| Validation-to-hints fusion | Exact hint and code parity, but +0.20% full-compile latency geomean, +0.53% heap, and +1.33% esbuild latency (p=0.015, n=8); bounded per-function fallback and packed shared headers could not make the extra validation state pay for the removed scan |
| Bitmask stack-flow terminator classification | Short run looked positive; 500 ms x 8 confirmation reversed to +0.90% geomean and +1.28% SQLite (p=0.005) |
| Specialized scalar load/store encoder helpers | -0.43% in the first interleaved run, but no individual workload was significant; an unchecked scaled-encoding variant regressed by +0.74%, so the extra surface was removed |
| Cached memory-zero address width and module type lookup | Short samples were noisy; the expanded cache regressed the focused geomean by +0.98% and Ruby by +1.35% (p=0.028), so it was removed |
| Common-constant `MovImm64` fast path | Initial 300 ms run was -0.92%; the 1 s confirmation fell to -0.23%, below the retention gate, so it was removed |
| Cached algebraic-discount eligibility | Focused hint geomean -0.03%, statistically flat; removed |
| Width-specific i32 home loads for memory addresses | Reduced code 2.0-4.6% and compile heap 0.12%, but compile latency was flat and json-as execution reproducibly regressed 4.81% (p<0.001); removed |
| Direct fixed-width memory comparisons | Initial backend geomean -0.62%; the six-pair 1 s confirmation was +0.01%, exactly flat; removed |
| Validation rare-reference lookup table | Short run looked positive; six-pair 1 s confirmation reversed to +1.11%; removed |
| Branch-hint reset only on `br_if` | +0.20% backend geomean; removed |
| Direct instance-context load/store calls | +0.36% backend geomean; removed |

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
`/tmp/arm64-current-vs-main-{base,candidate}-20260905.txt`,
`/tmp/arm64-current-exec-{main,candidate}-20260905.txt`, and
`/tmp/arm64-current-code-{main,candidate}-20260905.txt` on the measuring host.
They are intentionally not checked into the repository.

## Next ARM64 work

In the current large-module profile, scalar memory lowering is the largest
visible backend family: `ldst` is 24.7% cumulative, `memAddr` is 13.0%, calls
are 10.8%, materialization is 9.9%, and instance-context copying is 8.2%.
Validation is about 7.2% cumulative after the fused observer, while the
byte-backed hint scan is about 3.5%.
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
