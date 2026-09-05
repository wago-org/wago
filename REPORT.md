# Railshot compile-latency report

Measured through 2026-09-05 on native ARM64 and Rosetta AMD64. This is a stopping-point report for
`jairus/railshot-compile-latency`, comparing:

- Base: `main` at `c46f2129edb52e6f30f4d0bfc5ae105cfde0c84d`
- Branch implementation through: `85cd9086`
- Host: Apple M4 Max, macOS 26.6.2, Go 1.26.5, `darwin/arm64`

The AMD64 checkpoint below used the same host through Rosetta. Native Linux
AMD64 confirmation remains pending because `ssh hub@hub` timed out during this
measurement window.

## Pause checkpoint: current branch versus main

This is the requested review pause. The ARM64 numbers are the last exact native
checkpoint; the four commits after it are AMD64-only. The AMD64 numbers below
are a fresh comparison of current `71f82617` against pinned `main` using eight
fresh, interleaved 300 ms samples per revision on Rosetta. Negative latency is
better.

| Architecture and metric | Delta versus `main` |
|---|---:|
| ARM64 backend compile latency, five large modules | **-35.70%** |
| ARM64 full compile latency, five large modules | **-40.20%** |
| ARM64 backend compile heap | **-6.33%** |
| ARM64 full compile heap | **-1.62%** |
| ARM64 generated machine code | **-4.50%** |
| ARM64 execution latency, complete corpus | **-0.90%** |
| AMD64 backend compile latency, five large modules | **-25.58%** |
| AMD64 full compile latency, five large modules | **-32.98%** |
| AMD64 backend compile heap | **-0.28%** |
| AMD64 full compile heap | +0.24% (effectively flat) |
| AMD64 generated machine code | **0.00%** on all five modules |
| AMD64 execution latency, complete corpus | -0.25% (flat) |

### Fresh AMD64 compile results

| Corpus | Backend main | Backend branch | Delta | Main heap | Branch heap | Heap delta |
|---|---:|---:|---:|---:|---:|---:|
| json-as | 800.2 us | 615.2 us | **-23.12%** | 129.8 KiB | 130.4 KiB | +0.43% |
| Lua | 15.33 ms | 11.76 ms | **-23.32%** | 706.2 KiB | 703.1 KiB | -0.44% |
| SQLite | 55.82 ms | 46.48 ms | **-16.74%** | 2.002 MiB | 1.987 MiB | -0.78% |
| Ruby | 671.1 ms | 459.5 ms | **-31.53%** | 14.53 MiB | 14.46 MiB | -0.48% |
| esbuild | 422.3 ms | 286.8 ms | **-32.09%** | 17.78 MiB | 17.76 MiB | -0.12% |
| **Geomean** | 45.46 ms | 33.83 ms | **-25.58%** | 2.143 MiB | 2.137 MiB | **-0.28%** |

| Corpus | Full main | Full branch | Delta | Main heap | Branch heap | Heap delta |
|---|---:|---:|---:|---:|---:|---:|
| json-as | 1.518 ms | 1.026 ms | **-32.41%** | 186.7 KiB | 187.8 KiB | +0.55% |
| Lua | 25.43 ms | 17.25 ms | **-32.15%** | 1.015 MiB | 1.017 MiB | +0.20% |
| SQLite | 95.18 ms | 69.62 ms | **-26.86%** | 3.652 MiB | 3.660 MiB | +0.21% |
| Ruby | 1.090 s | 688.4 ms | **-36.81%** | 32.36 MiB | 32.43 MiB | +0.22% |
| esbuild | 707.7 ms | 451.4 ms | **-36.22%** | 70.46 MiB | 70.49 MiB | +0.03% |
| **Geomean** | 77.70 ms | 52.07 ms | **-32.98%** | 4.341 MiB | 4.352 MiB | +0.24% |

The newest change replaces AMD64's detailed per-op arena simulation with the
same coarse bounded body estimate already proven on ARM64. Against its immediate
predecessor it improves backend compilation **5.57%** and full compilation
**3.11%**, while reducing backend/full heap **0.38%/0.21%**. It shrinks the
retained `funcHints` record from 32 to 24 bytes and deletes 586 lines. The five
backend improvements were all significant (`p<=0.007`); full-compilation Lua
and esbuild were favorable but not individually significant in that sample.

The three preceding AMD64 emission/scanner changes improved their immediate
predecessors as follows:

| Incremental AMD64 change | Backend compile | Full compile | Heap | Code |
|---|---:|---:|---:|---:|
| Direct four-byte `imm32` append | **-1.45%** | **-2.16%** | unchanged | identical sizes |
| Specialized one-to-four-byte `emit` | **-1.59%** | **-1.37%** | unchanged | identical sizes |
| Dispatch hint boundaries with exact opcodes | **-1.60%** | **-1.41%** | unchanged | identical sizes |
| Coarse bounded arena estimate | **-5.57%** | **-3.11%** | -0.38% / -0.21% | identical sizes |

Generated AMD64 code remains 57,470 B, 889,172 B, 3,634,105 B,
35,155,402 B, and 26,612,624 B for json-as, Lua, SQLite, Ruby, and
esbuild respectively, exactly matching `main` in size.

### Fresh AMD64 execution results

The complete runnable corpus used eight fresh interleaved 100 ms samples. Its
geomean moved from 4.807 us to 4.795 us (**-0.25%**, statistically flat), with
**0 B/op and 0 allocs/op** on both revisions. Indirect-call dispatch improved
5.07%. SHA-256 and raytrace initially crossed `p<0.05` in the short broad run;
a dedicated 12-pair alternating-order 300 ms confirmation found both flat and
favorable (SHA-256 -1.35%, `p=0.101`; raytrace -0.29%, `p=0.378`).

### Validation and raw data

- Native ARM64 `go test ./...`: pass.
- Rosetta AMD64 backend, shared, frontend, and Wasm tests: pass.
- Five large AMD64 generated-code sizes: exact parity with `main`.
- Native AMD64 remains pending: `ssh hub@hub` timed out again at this checkpoint.
- Rosetta cannot qualify the guard-page differential suite: its host-feature
  detection disables SIMD and produces the known explicit/guard discrepancy.

Raw captures are:

- `/tmp/amd64-current-vs-main-compile-{base.RsQzXJ,candidate.xa8whx}`
- `/tmp/amd64-current-vs-main-exec-{base.pE13Cp,candidate.CkfETy}`
- `/tmp/amd64-current-vs-main-exec-focus-{base.Fg3ZD4,candidate.9WKEq5}`
- `/tmp/amd64-coarse-production-focused-{base.fVaqGL,candidate.FI7Xhg}`
- `/tmp/amd64-coarse-production-full-{base.QYM8ss,candidate.Sj4Aiz}`

### Validation observation fast path

The latest retained cross-architecture change removes the full validation-fact
observer from ordinary scalar instructions. The validation loop now records the
common per-opcode flag directly and calls the payload classifier only for the
small set of instructions whose immediates affect retained module analysis.

A 12-pair, alternating-order 500 ms Rosetta AMD64 confirmation against the
immediate predecessor measured the five-large-module full-compilation geomean
at **-1.51%**, with allocated heap and allocation counts unchanged. Ruby
improved **1.90%** (`p=0.028`); the other four modules were favorable or flat.
The old observer disappeared from the esbuild CPU profile. Native ARM64
`go test ./...` passes, and the new table-consistency test verifies that the
fast-path classification exactly matches the authoritative payload switch.

An attempted follow-on shortcut that decoded and stepped simple opcodes
directly in the validation loop was rejected. Its longer 12-pair confirmation
measured **+0.39%** full-compilation geomean, despite an initially favorable
short run. That code is not present in the branch.

Additional raw captures are:

- `/tmp/amd64-validation-payload-{base.2ukBeY,candidate.QOkClI}`
- `/tmp/amd64-validation-payload-confirm-{base.OKaxgn,candidate.4ho7uU}`
- `/tmp/amd64-validation-payload-esbuild.pprof`
- `/tmp/amd64-validation-simple-{base.0QO6jX,candidate.bBuMa9}`
- `/tmp/amd64-validation-simple-confirm-{base.mpzkEA,candidate.JoxoEO}`

## Current result

| Metric | Delta versus `main` |
|---|---:|
| End-to-end compile latency, older all-corpus checkpoint | **-9.38%** |
| End-to-end compile latency, large-module geomean | **-40.20%** |
| Backend-only compile latency, large-module geomean | **-35.70%** |
| Backend-only compile latency, older all-corpus checkpoint | **+0.86%** |
| End-to-end compile heap, large-module geomean | **-1.62%** |
| End-to-end compile heap, older all-corpus checkpoint | **+1.07%** |
| Backend-only compile heap, large-module geomean | **-6.33%** |
| End-to-end compile allocations, large-module geomean | **-19.00%** |
| Backend-only compile allocations, large-module geomean | **-44.87%** |
| End-to-end compile allocations, older all-corpus checkpoint | **+1.66%** |
| Backend-only compile allocations, older all-corpus checkpoint | **+0.16%** |
| Generated ARM64 machine-code bytes | **-4.50%** large-module geomean; up to **-7.69%** |
| Execution latency, complete-corpus geomean | **-0.90%** |
| Execution allocations | **0 B/op, 0 allocs/op** on both revisions |

## Current AMD64 checkpoint

Eight interleaved 300 ms samples per revision compare the retained branch at
`5d9b1f2d` with pinned `main` at `c46f2129`. Negative latency is better.

| Metric | Delta versus `main` |
|---|---:|
| Backend-only compile latency, large-module geomean | **-16.93%** |
| End-to-end compile latency, large-module geomean | **-27.98%** |
| Backend-only compile heap, large-module geomean | +0.11% |
| End-to-end compile heap, large-module geomean | +0.45% |
| Backend-only allocation count, large-module geomean | +0.40% |
| End-to-end allocation count, large-module geomean | +0.25% |
| Generated AMD64 machine-code bytes | **0.00%** on all five large modules |
| Execution latency, complete runnable corpus geomean | -0.24% (flat) |
| Execution allocations | **0 B/op, 0 allocs/op** on both revisions |

| Corpus | Backend main | Backend branch | Latency delta | Heap delta | Allocation delta |
|---|---:|---:|---:|---:|---:|
| json-as | 800.9 us | 688.4 us | **-14.05%** | +0.54% | +1.65% |
| Lua | 15.32 ms | 12.73 ms | **-16.90%** | +0.02% | +0.28% |
| SQLite | 55.34 ms | 49.59 ms | **-10.40%** | +0.00% | +0.08% |
| Ruby | 658.0 ms | 512.7 ms | **-22.09%** | +0.00% | +0.02% |
| esbuild | 412.2 ms | 327.0 ms | **-20.66%** | flat | flat |
| **Geomean** | **44.98 ms** | **37.37 ms** | **-16.93%** | **+0.11%** | **+0.40%** |

| Corpus | Full compile main | Full compile branch | Latency delta | Heap delta | Allocation delta |
|---|---:|---:|---:|---:|---:|
| json-as | 1.501 ms | 1.101 ms | **-26.64%** | +0.61% | +1.04% |
| Lua | 25.12 ms | 18.24 ms | **-27.39%** | +0.52% | +0.16% |
| SQLite | 94.53 ms | 70.37 ms | **-25.55%** | +0.64% | +0.05% |
| Ruby | 1.100 s | 754.4 ms | **-31.42%** | +0.44% | +0.01% |
| esbuild | 769.8 ms | 548.4 ms | **-28.77%** | +0.06% | flat |
| **Geomean** | **78.69 ms** | **56.67 ms** | **-27.98%** | **+0.45%** | **+0.25%** |

The complete runnable execution corpus used eight interleaved 100 ms samples.
Its geomean moved from 4.715 us to 4.704 us (**-0.24%**, statistically flat).
The only significant row was indirect-call dispatch at **-5.72%**; no workload
significantly regressed. The five large-module code sizes matched exactly:
57,470 B, 889,172 B, 3,634,105 B, 35,155,402 B, and 26,612,624 B for json-as,
Lua, SQLite, Ruby, and esbuild respectively.

The exact current-HEAD large-corpus comparison used eight fresh interleaved
300 ms samples per revision:

| Corpus | Full compile main | Full compile branch | Latency delta | Heap delta | Allocation delta |
|---|---:|---:|---:|---:|---:|
| json-as | 1.363 ms | 0.844 ms | **-38.10%** | -0.76% | -12.06% |
| Lua | 22.77 ms | 14.20 ms | **-37.65%** | -2.50% | -9.75% |
| SQLite | 86.95 ms | 52.59 ms | **-39.51%** | -2.84% | -11.29% |
| Ruby | 966.3 ms | 565.9 ms | **-41.44%** | -0.87% | -6.58% |
| esbuild | 629.0 ms | 351.8 ms | **-44.07%** | -1.13% | -46.98% |
| **Geomean** | **69.66 ms** | **41.66 ms** | **-40.20%** | **-1.62%** | **-19.00%** |

The matching backend-only geomean improved from 41.90 ms to 26.94 ms
(**-35.70%**). Every per-corpus backend and end-to-end latency result was
significant at `p<0.001`. Backend allocated heap improved **6.33%** by geomean;
every large corpus used less backend heap than `main`.

| Corpus | Backend main | Backend branch | Latency delta | Heap delta | Allocation delta |
|---|---:|---:|---:|---:|---:|
| json-as | 751.7 us | 529.9 us | **-29.51%** | -2.51% | -37.59% |
| Lua | 13.92 ms | 9.511 ms | **-31.66%** | -5.10% | -33.69% |
| SQLite | 53.46 ms | 36.44 ms | **-31.85%** | -9.21% | -33.65% |
| Ruby | 603.9 ms | 362.2 ms | **-40.01%** | -4.90% | -19.44% |
| esbuild | 377.3 ms | 210.6 ms | **-44.19%** | -9.74% | -76.97% |
| **Geomean** | **41.90 ms** | **26.94 ms** | **-35.70%** | **-6.33%** | **-44.87%** |

The inline-analysis change is representation-only: all five final compiled-code
buffers matched its immediate predecessor in both byte length and SHA-256.

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
| Pair instance-context transfers | **-0.61%** | not rerun | effectively flat | **-0.78%** code on the large-module sample; cross-instance execution **-4.05%** geomean |
| Skip bounds-diagnostic control scans when stats are disabled | **-3.27%** | not rerun | unchanged | Exact code-byte parity |
| Batch four loop-alignment NOPs | **-0.55%** | not rerun | unchanged | Exact code-byte parity |
| Skip diagnostic-only loop-body rescans when stats are disabled | **-6.44%** | not rerun | **-0.80% backend heap** | Exact code-byte parity |
| Limit default ARM64 inlining to 16-byte helper bodies | not isolated | **-2.43%** | **-0.44% heap; -2.71% allocs** | **-2.42% code; -0.77% execution** versus the prior 160-byte limit |
| Skip unreachable finalizer fragment cursors | **-1.85%** | **-2.99%** | unchanged | Exact generated-code behavior by construction; complete-corpus sizes identical |
| Skip merge snapshots when no local register is pinned | **-5.58%** | included below | **-0.93% heap; -18.16% allocs** | Exact code-size parity across the checked corpus |
| Skip local call scans when no clobbered pin exists | **-1.77%** | included below | unchanged | Exact code-size parity across the checked corpus |
| Inline the complete 64-local merge snapshot | **-1.33%** | **-5.06% for the three-change series** | **-2.52% backend heap; -19.34% backend allocs** incrementally | Exact code-size parity; complete execution corpus passed |
| Reuse repeated host-adapter encodings | **-0.60%** | **-1.25%** | unchanged | Exact large-corpus code-size and SHA-256 parity; complete execution corpus passed |
| Fast ARM64 inline caller analysis | **-0.94%** | **-0.77%** | **-0.45% backend heap; -8.63% backend allocs** | Exact large-corpus code-size and SHA-256 parity |
| Replace detailed ARM64 arena prediction with a coarse bounded estimate | **-3.69%** | **-2.88%** | **-0.74% backend; -0.28% full heap** | Exact large-corpus code-size and SHA-256 parity |
| Read bytecode bytes without nested cursor calls | **-2.61%** | **-2.43%** | unchanged | Exact large-corpus code-size and SHA-256 parity |
| Fast-path one-byte `U32` LEBs | **-1.73%** | **-1.92%** | unchanged | Exact large-corpus code-size and SHA-256 parity |
| Fast-path one-byte `I32` LEBs | **-0.79%** | **-0.17%** (flat) | unchanged | Exact large-corpus code-size and SHA-256 parity |

The latest ARM64 change removes the per-op arena-node simulation from hint
construction. Small ordinary functions use a constant-time estimate derived
from body bytes and locals; large, multi-value, inline, and expanded-lowering
cases retain the conservative 256-node fallback. This shrinks `funcHints` from
32 to 28 bytes and deletes more than 500 lines of prediction machinery. Against
the immediately preceding branch revision, all five large corpora improved in
both backend-only and full compilation. A 64-KiB repeated-expression outlier
improved 19.49%; the tuned estimate also reduced heap on the small expression
fixtures without changing their generated code.

The faster worker path exposed a pre-existing native-size accounting race in
the per-worker host-adapter template cache: cached alignment padding was
occasionally attributed to the internal function. Cache hits now record that
padding explicitly. Compiled bytes were already deterministic; the fix makes
the diagnostic attribution deterministic as well. The worker determinism test
passed 20 consecutive repetitions, the complete ARM64 package passed, and
`go test ./...` passed at this checkpoint.

Raw benchmark captures for this checkpoint are under `/tmp` as
`arm64-coarse-arena-vs-main-{base,candidate}.txt`,
`arm64-coarse-arena-{backend,full}-{base,candidate}.txt`,
`arm64-coarse-arena-small-{base,candidate}.txt`, and
`arm64-coarse-arena-outlier-{base,candidate}.txt`.

An opt-in full-pipeline optimization-ablation benchmark found inlining to be
the remaining broad compile-cost outlier. Disabling it entirely improved full
compilation substantially, but regressed `many_funcs` execution by 8.58% and
was rejected. A sweep of 0, 8, 16, 24, 32, 48, 64, 96, and 160-byte body
limits selected 16 bytes as the measured Pareto point. Against 160 bytes, it
reduced full compile latency 2.43%, compile heap 0.44%, allocation count 2.71%,
and generated code 2.42%, while improving complete-corpus execution 0.77% with
`many_funcs` flat. The default remains overrideable for diagnostics.

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

The strongest and most stable result is on large real modules. The exact
current comparison is the eight-pair table above: end-to-end compile latency is
37.7-44.1% lower on json-as, Lua, SQLite, Ruby, and esbuild, and backend-only
compilation is 29.5-44.2% lower on the same group. The all-corpus
backend geomean includes fresh-process micro modules whose 7-59% confidence
intervals overwhelm their tens-of-microseconds signal; it is reported rather
than filtered, but is not evidence of a backend regression. The branch
currently spends a small amount of heap on compact validation analysis and
parallel hint orchestration. The large-module geomean is 40.20% faster end to
end with 1.62% less allocated heap and 19.00% fewer allocations. Every latency
result is significant at p<0.001; esbuild full-compile heap is 1.10% below
`main`.

Before the address-proof change, generated function code had exactly the same
size across the entire corpus. The address-proof change intentionally removes
redundant instructions; its focused execution measurements found improvements
on memory-heavy rows and no confirmed regression. The older complete execution
table below remains the representation-only checkpoint at `e5b2431a`, not a
claim about exact instruction parity at current HEAD.

## Current generated code and execution

The retained changes that intentionally alter generated code are reuse of
already-proven-clean memory32 addresses, paired instance-context transfers, and
the tighter inline-body budget. The bounds-diagnostic change is byte-identical
to its predecessor. Against
pinned `main`, current generated ARM64 code is 4.50% smaller by geomean across
the five large modules:

| Corpus | Main code | Branch code | Delta |
|---|---:|---:|---:|
| json-as | 76,472 B | 74,856 B | -2.11% |
| Lua | 1,000,820 B | 962,820 B | -3.80% |
| SQLite | 3,957,880 B | 3,750,536 B | -5.24% |
| Ruby | 39,294,784 B | 37,894,368 B | -3.56% |
| esbuild | 31,288,340 B | 28,883,520 B | -7.69% |

A fresh complete execution-corpus run at `b9e5db49` used eight
interleaved 100 ms samples per revision. The geomean improved **0.90%**, with
**0 B/op and 0 allocs/op** on both revisions. Every statistically significant
movement was an improvement: linked-list -2.95%, nbody -3.32%, matmul -4.86%,
CRC32 -9.54%, SHA-256 -5.22%, json-as deserialization -3.01%, json-as SIMD
serialization -1.96%, and UTF SIMD conversion -3.24%. `many_funcs` was flat.

## Large-module memory detail

| Corpus | Backend heap main | Backend heap branch | Full heap main | Full heap branch |
|---|---:|---:|---:|---:|
| json-as | 51.83 KiB | 50.53 KiB | 108.8 KiB | 108.1 KiB |
| lua | 509.5 KiB | 483.6 KiB | 842.6 KiB | 821.6 KiB |
| sqlite3 | 1.105 MiB | 1.003 MiB | 2.755 MiB | 2.677 MiB |
| ruby | 7.364 MiB | 7.003 MiB | 25.19 MiB | 24.97 MiB |
| esbuild | 7.353 MiB | 6.637 MiB | 60.04 MiB | 59.37 MiB |

## Older complete compile corpus

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

## Older representation-only execution checkpoint

This table predates the intentional address-proof, paired-transfer, and inline
budget code changes. Each row is the median of eight interleaved 100 ms samples
from the earlier byte-identical checkpoint; the current execution result is the
fresh summary in "Current generated code and execution" above.

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

Implementation head `43a29b50` has 53 committed changesets over pinned `main`
and 33 retained implementation changes:

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
25. Paired ARM64 instance-context loads and stores.
26. Skipped diagnostic-only loop and bounds-hoist scans when statistics are
    disabled.
27. Limited default ARM64 inlining to 16-byte helper bodies after an exhaustive
    threshold sweep, with an opt-in optimization-ablation benchmark.
28. Removed unreachable fragment-cursor updates from the finalized peephole
    scans while preserving the opaque-fragment early return.
29. Skipped local merge-state construction for call-making functions without
    any register-homed locals.
30. Skipped call-boundary local scans when the clobber masks do not intersect
    either pinned-local register bank.
31. Replaced heap-backed local merge snapshots with two inline 64-bit words,
    removing their pool and shrinking each cold merge sidecar by 16 bytes.
32. Reused repeated ARM64 host-adapter encodings through one bounded 256-byte
    per-worker template after a function type repeats, replacing instruction-by-
    instruction re-emission with one copy while preserving exact layout.
33. Skipped inline caller analysis for call-free functions, decoded common caller
    immediates directly, and replaced the ordinary loop-control slice with a
    fixed 64-bit stack plus a deep-control fallback.

Implementation source delta, excluding this report:

```text
54 files changed, 2,682 insertions, 1,016 deletions
```

The larger source increase is primarily validation-analysis structure, tests,
and duplicated architecture-specific integration. Only the clean-address,
paired instance-context, and inline-budget changes intentionally alter generated
code.

## Rejected ARM64 probes

These were measured and removed rather than retained speculatively:

| Probe | Result |
|---|---|
| Unsafe 32-bit instruction store in `Asm.word` | Existing append lowering already emits one store; unsafe form added work |
| Saturated `uint32` local hotness | +1.25% focused geomean; esbuild +1.58% |
| Lookup table for loop weighting | Unstable and slower overall |
| Pending-memory-reference counter | +0.44% backend geomean; Ruby +3.35% (p=0.038); no heap benefit |
| Compiler-local replacement for the shared bytecode reader | +15.45% focused geomean; json-as +70.84%. This was a larger reader/state substitution, not the later retained direct `Reader.Byte` cursor increment |
| Dense function-signature pointer cache | +13.47% focused geomean, +1.07% heap |
| Cached imported-function boundary | +5.32% focused geomean; json-as +26.93% |
| Heavy-first scheduling | -0.04%; below measurement value |
| Validation-to-hints fusion | Exact hint and code parity, but +0.20% full-compile latency geomean, +0.53% heap, and +1.33% esbuild latency (p=0.015, n=8); bounded per-function fallback and packed shared headers could not make the extra validation state pay for the removed scan |
| Bitmask stack-flow terminator classification | Short run looked positive; 500 ms x 8 confirmation reversed to +0.90% geomean and +1.28% SQLite (p=0.005) |
| Specialized scalar load/store encoder helpers | -0.43% in the first interleaved run, but no individual workload was significant; an unchecked scaled-encoding variant regressed by +0.74%, so the extra surface was removed |
| Cached memory-zero address width and module type lookup | Short samples were noisy; the expanded cache regressed the focused geomean by +0.98% and Ruby by +1.35% (p=0.028), so it was removed |
| Explicit `MovImm32` branch classification | Exact randomized encoding parity, but +0.41% backend geomean |
| Conditional telemetry defers | +0.08% backend geomean; below measurement value |
| Skip trap-site sort for single-function groups | Short run was -0.74%; 1 s confirmation reversed to +0.14%, so it was removed |
| Common-constant `MovImm64` fast path | Initial 300 ms run was -0.92%; the 1 s confirmation fell to -0.23%, below the retention gate, so it was removed |
| Cached algebraic-discount eligibility | Focused hint geomean -0.03%, statistically flat; removed |
| Width-specific i32 home loads for memory addresses | Reduced code 2.0-4.6% and compile heap 0.12%, but compile latency was flat and json-as execution reproducibly regressed 4.81% (p<0.001); removed |
| Direct fixed-width memory comparisons | Initial backend geomean -0.62%; the six-pair 1 s confirmation was +0.01%, exactly flat; removed |
| Validation rare-reference lookup table | Short run looked positive; six-pair 1 s confirmation reversed to +1.11%; removed |
| Branch-hint reset only on `br_if` | +0.20% backend geomean; removed |
| Direct instance-context load/store calls | +0.36% backend geomean; removed |
| Zero-register store for unpinned zero-valued locals | +0.68% backend geomean, with Lua +1.52% and SQLite +1.63%; removed despite a 0.13% heap reduction |
| Memarg-only hint classification for scalar loads/stores | -0.16% backend geomean, but focused hint scans were +0.12% and flat; below measurement value |
| Split hot scaled load/store helpers | +0.30% backend geomean, with Lua +2.32%; removed |
| `binary.LittleEndian.AppendUint32` instruction emission | +1.25% backend geomean, with Ruby +3.17%; the existing four-byte append lowers better |
| Stable cost-bucket parallel scheduling | Reduced `p4` allocation counts 5.24% by geomean and up to 20.19%, but did not improve latency and regressed Ruby `p4` by 3.89%; removed under the no-memory-only-landing gate |
| Cache frontend validation-analysis identity once per pass | Broad full-compile sample was -1.11% but nonsignificant; a longer `many_funcs`/json-as confirmation was +0.13% and flat, with identical heap and allocations; removed as below measurement value |
| Clear branch-hint state only for `br_if` | Combined backend/full large-module geomean was -0.23%, with every row nonsignificant and resources unchanged; removed as below measurement value |
| Remove fixed poll-free loop phase padding | Combined backend/full large-module geomean was +0.06% and flat; minor heap/code-footprint savings did not buy latency, so the execution-layout policy was retained |
| Reslice-and-`PutUint32` ARM64 instruction emission | +3.01% combined geomean; every backend corpus regressed 2.1-4.7%, showing the compiler already lowers the existing four-byte append more efficiently |
| Pair adjacent frame-local binary operands with `LDP` | Reduced backend heap slightly but compiled +0.48% slower by geomean with no significant latency win; removed under the no-memory-only gate |
| Disable branch folding | Full compile -1.57% and code +0.89%, but execution +0.54% geomean with fib_iter +26.03%, globals +53.96%, spectralnorm +6.77%, and matmul +5.69%; retained the optimization |
| Record bounded branch-pair candidates during target collection | Exact output/resources, but combined compile latency -0.02% and every corpus nonsignificant; removed |
| Disable store/load forwarding | Full compile -3.60% with equal code size, but execution +0.50% geomean and json-as serialization +17.96%; retained the optimization |
| Fuse branch-pair and store/load scans | Exact output/resources, but +0.17% combined compile latency with every corpus nonsignificant; removed |
| Allocate local merge snapshots in 16-buffer slabs | Allocation count -17.41%, but compile latency +2.36% and heap +0.25%; replaced by the inline fixed snapshot |
| Reserve and clear `br_table` placeholders in one span | +2.41% backend geomean with unchanged heap and allocations; the existing four-byte append loop lowers better |
| Remove the scan-local entry-initialization mirror | +0.08% backend geomean; every corpus nonsignificant, heap and allocations identical; retained as a diagnostic mirror only |
| Clear only initialized control-sidecar prefixes | Initial backend run was -0.56%; the 12-pair 500 ms confirmation fell to -0.36%, with every latency row nonsignificant and heap/allocations effectively unchanged; removed as below measurement value |
| Replace module-global scans with a bounded sorted view | +0.49% backend geomean with every corpus nonsignificant and resources unchanged; instruction emission, not membership scanning, accounted for the profiled synchronization cost |
| Trap-specific `uint32` immediate materialization | -0.36% backend geomean with every corpus nonsignificant and resources unchanged; removed rather than risk expanding logical-immediate cases in cold stubs |
| Recycle nodes from terminal pure-drop trees | Dedicated 512-tree stress improved latency 6.89%, heap 86.33%, and allocations 22.73%, but all five real corpora slowed and the backend geomean regressed 1.15% with no corpus resource benefit; removed pending a giant-function-only admission policy |
| Route contiguous integer opcodes through a bounded direct table | +1.98% backend geomean, including significant Lua (+2.83%) and esbuild (+1.55%) regressions; Go's existing large switch dispatch remained faster |
| Fuse the two ARM64 inline caller scans | Removed more allocation traffic, but keeping loop-proof state through the full combined scan regressed esbuild by 2.76%; retained the faster decomposed scans with call-free gating and compact control state |
| Share complete trap bodies in ordinary mode | Shrunk generated code by 2.12-5.81% across the five large modules, but backend compile latency improved only 0.24% and was statistically flat. Complete execution improved 0.67% overall, but a longer confirmation found a significant 2.29% `linked_list` regression; retained sharing only in the opt-in compact mode |
| Skip trap-site sorting when each function group is already uniform | Backend compile geomean improved 0.25%, but every corpus was statistically flat and heap/allocations were identical; removed as below measurement value |
| Double the initial ARM64 instruction-buffer reservation | Backend compile geomean regressed 0.05%, statistically flat, with unchanged heap and allocations; the existing body-times-four reservation already covers the useful range |
| Make compact trap sharing the default | The existing compact mode reduced heap 1.18% and allocation count 13.89%, but regressed backend compile latency 4.27%; it remains an explicit footprint trade rather than the default |
| Retain validation's resolved component types and consume them in ARM64 lowering | Replaced the frozen validation map with a direct dense traversal and bypassed repeated local function-signature lookup during lowering. Full compile regressed 0.88% by geomean and heap rose 0.42%; all five latency rows were statistically flat, so the type plan and backend integration were removed |
| Split checked and unchecked local-hotness updates in the byte scanner | Focused global/local hint scans regressed 1.05% by geomean with identical heap and allocations; the compiler already eliminates enough of the inlined checks, so the split was removed |
| Gate imported-tail safety scanning on validation's tail-call fact | The public feature set is already narrowed from module requirements, so the proposed fact check was redundant. Full compile regressed 0.28% by geomean, statistically flat, with exactly unchanged resources; removed |
| Bypass hint-switch dispatch for the dense numeric opcode interval | Focused synthetic hint scans initially improved 0.59%, but the five-corpus backend confirmation regressed 1.41%, including SQLite +4.12% and esbuild +1.97%; the extra pre-switch branch disturbed the much broader opcode mix and was removed |
| Track pending memory references to bypass empty pre-store stack scans | Five-corpus backend compile regressed 0.91%, with SQLite significantly slower by 1.26%, while heap and allocations were unchanged; the counter maintenance cost exceeded the avoided scans, so it was removed |
| Select spill victims from the fixed register-owner table | Reused variant-dead node storage for an exact physical-stack order token and preserved the previous deepest-owner choice. Five-corpus backend compile regressed 1.33%, with Lua +2.10% and Ruby +2.19% significant; maintaining and comparing order tokens cost more than the rare list walks, so it was removed |
| Reset branch-hint state only for branch opcodes | Removed one field store from ordinary opcode dispatch, but the five-corpus backend result was only -0.19% and every row was statistically flat; removed as below measurement value |
| Reserve `br_table` placeholders with one bulk zero extension | A broad screen was -0.59%, but the one-second Lua/Ruby/esbuild confirmation was exactly flat at +0.01% geomean (Lua +0.44%, Ruby +0.90%, esbuild -1.29%); retained the existing four-byte append loop |
| Fast-path one-byte `i64` immediates | Backend geomean was -0.46%, but every corpus was statistically flat and resources were unchanged; removed as below measurement value |
| Fast-path one-byte signed block types | Backend geomean regressed 0.30%, with every corpus statistically flat and resources unchanged; removed |
| Reserve overwritten `br_table` entries by reslicing retained capacity | Backend geomean regressed 0.33%, including a significant 1.31% Lua regression; the redundant-looking zero writes help rather than hurt the later patching path |

## Rejected AMD64 probes

| Probe | Result |
|---|---|
| Track pending memory references to bypass empty allocator scans | Backend geomean regressed 1.82%; Ruby regressed 2.83% and esbuild 1.97% significantly, with unchanged heap and allocations. The exact ownership-safe counter was removed. |
| Replace the pointer-rich 56-byte operand node with packed child/link IDs | Reached the intended 32-byte pointer-free node and cut serial backend heap 33.09% by geomean (18.73-45.01% per large module), with identical generated-code sizes. It also regressed backend latency 3.89% by geomean, including significant 3.11-4.77% regressions on Lua, SQLite, Ruby, and esbuild, so it was removed under the latency-first gate. |

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
`/tmp/arm64-adapter-cache-vs-main-{base,candidate}.txt`,
`/tmp/arm64-adapter-template-long-{base,candidate}.txt`,
`/tmp/arm64-adapter-template-full-{base,candidate}.txt`,
`/tmp/arm64-control-prefix-long-{base,candidate}.txt`,
`/tmp/arm64-module-global-view-{base,candidate}.txt`,
`/tmp/arm64-trap-u32-{base,candidate}.txt`,
`/tmp/arm64-terminal-recycle-{stress,corpus}-{base,candidate}.txt`,
`/tmp/arm64-integer-dispatch-{base,candidate}.txt`,
`/tmp/arm64-current-ablation.txt`,
`/tmp/arm64-inline-fast-collect-long-{base,candidate}.txt`,
`/tmp/arm64-inline-fast-collect-full-{base,candidate}.txt`,
`/tmp/arm64-inline-fast-vs-main-{base,candidate}.txt`,
`/tmp/arm64-inline16-vs-main-exec5-{base,candidate}.txt`, and
`/tmp/arm64-inline16-code-{main,current}.txt`,
`/tmp/arm64-shared-trap-default-{base,candidate}.txt`,
`/tmp/arm64-shared-trap-exec-{base,candidate}.txt`,
`/tmp/arm64-shared-trap-exec-confirm-{base,candidate}.txt`,
`/tmp/arm64-trap-sort-uniform-{base,candidate}.txt`, and
`/tmp/arm64-asm-reserve8-{base,candidate}.txt`,
`/tmp/arm64-resolved-dense-{base,candidate}.txt`, and
`/tmp/arm64-resolved-types-{base,candidate}.txt`,
`/tmp/arm64-hotness-at-{base,candidate}.txt`, and
`/tmp/arm64-tail-scan-gate-{base,candidate}.txt`,
`/tmp/arm64-numeric-hint-fast-{base,candidate}.txt`, and
`/tmp/arm64-numeric-hint-fast-backend-{base,candidate}.txt`, and
`/tmp/arm64-pending-memref-{base,candidate}.txt`, and
`/tmp/arm64-reg-owner-{base,candidate}.txt`, and
`/tmp/arm64-branch-state-{base,candidate}.txt`,
`/tmp/arm64-reader-byte-clean-{base,candidate}-0905.txt`,
`/tmp/arm64-reader-u32-{base,candidate}-0905.txt`,
`/tmp/arm64-reader-i32-{base,candidate}-0905.txt`, and
`/tmp/arm64-reader-final-vs-main-{base,candidate}-0905.txt`, and
`/tmp/arm64-brtable-zero-{base,candidate}-0905.txt` plus
`/tmp/arm64-brtable-zero-confirm-{base,candidate}-0905.txt`,
`/tmp/arm64-reader-i64-{base,candidate}-0905d.txt`,
`/tmp/arm64-reader-s33-{base,candidate}-0905.txt`, and
`/tmp/arm64-brtable-reslice-{base,candidate}-0905.txt`, plus
`/tmp/amd64-pending-count2-{base,candidate}-0905.txt`,
`/tmp/amd64-nodeid-backend-{base,candidate}.txt`,
`/tmp/amd64-current-vs-main-{backend,full,exec}-{base,candidate}.txt`, and
`/tmp/amd64-current-vs-main-code-{base,candidate}.txt` on the measuring host.
They are intentionally not checked into the repository.

## Next ARM64 work

In the fresh current esbuild profile, instruction emission dominates:
`Asm.word` is 78.9% flat, `memAddr` is 33.1% cumulative, scalar stores are
29.5%, materialization is 18.5%, scalar loads are 14.6%, and local writes are
12.5%. Hint construction is now about 2.8% cumulative. `br_table` accounts for
6.6% cumulative and 4.3% flat, but replacing its placeholder append loop with
a bulk zero extension was exactly flat in the longer confirmation. The raw
profile is `/tmp/arm64-reader-final-profile.pprof` with text summary
`/tmp/arm64-reader-final-profile.txt`.
Simple load/store helper specialization, unchecked scaled encoders, and module
memory-type caches did not clear the retention gate. A complete
validation-to-hints fusion was also implemented and rejected because it
increased latency and heap, so the next work should remain narrow and
profile-led rather than retaining more summary state. Follow-up probes also
ruled out generic memarg-only hint classification, alternative instruction-word
appends, zero-register local stores, and cost-bucket scheduling. The ARM64 first
pass has now found further Pareto improvements in the inlining budget and the
finalized peephole loops. The local-state pass then removed unobservable scans
and replaced thousands of tiny snapshot allocations with a 16-byte inline
value. Independent ablations prove that `branch-fold` and `store-load-fwd`
remain execution-critical, while candidate inventories, snapshot slabs, and a
fused finalizer scan did not improve compilation. The next ARM64 pass should
therefore be driven by a fresh current profile rather than broader finalizer or
pooling rewrites.

The latest pass found a bounded re-emission cost outside those rejected areas:
after one function type repeats, each worker retains at most one 256-byte host-
adapter template and copies it for later functions of that exact immutable type.
A 12-pair 500 ms backend confirmation improved the five-module geomean by 0.60%,
and an eight-pair full-pipeline run improved it by 1.25%. Heap and allocations
were unchanged. All five large-module code sizes and SHA-256 hashes matched the
immediate pre-cache implementation exactly, and the
serial/parallel, full-repository, and execution-corpus suites passed.

The current stopping-point pass then removed ordinary heap-backed loop tracking
from inline caller analysis, bypassed that analysis for call-free functions, and
fast-decoded common immediates. Its 12-pair backend confirmation improved the
large-module geomean by 0.94% and cut backend allocations by 8.63%; the separate
full-pipeline run improved 0.77%. The final direct comparison against pinned
`main` is the table at the top. The ARM64 package suite, `go test ./...` in both
the root and benchmark modules, `TestCorpus`, and `make docs-check` all pass.

The near-term acceptance gates are:

1. Preserve exact generated machine-code bytes.
2. Improve large-module backend latency by at least another 3%.
3. Return full-pipeline heap to at most `main` before expanding the summary.
4. Keep tiny-module p50 within 1.5% once measured with a longer adaptive run.
