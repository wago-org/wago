Fresh branch replacing #563, rebased onto current `main`. The patch stack is range-diff equivalent to the original branch.

## Outcome

Cuts Railshot compilation latency without changing generated native code.

| Measurement | ARM64 | AMD64 |
|---|---:|---:|
| Backend compile geomean | **-20.36%** | **-16.32%** |
| Public decode+validate+compile geomean | **-24.83%** | **-26.37%** |
| Backend heap geomean | +0.53% | +1.07% |
| Public-pipeline heap geomean | +1.26% | +1.85% |
| Generated native code | **0.00%** | **0.00%** |
| Execution geomean | +0.17% | -1.90% |

The execution directions differ despite byte-identical generated code, so those
sub-2% aggregate movements are measurement drift rather than a codegen change.

Base: `c46f2129edb52e6f30f4d0bfc5ae105cfde0c84d`

Head: `b467c044`

## What changed

- Keep diagnostics timing off ordinary compilation unless stats or explain output is requested.
- Produce one compact validation analysis and reuse it for feature admission, calls, GC, roots, thread boundaries, and dynamic-reference checks.
- Decode common hint immediates directly on both backends.
- Scan large modules' function hints in parallel with deterministic lowest-index errors and exact serial/parallel parity.
- Cache import-resolved memory/global types for modules with at least 16 KiB of function bodies; tiny modules keep direct lookups.
- Fuse AMD64 inline-caller admission into one body walk and fast-path its common immediates.

There is no new IR, no retry tier, no unbounded optimizer state, and no public policy knob.

## Backend compilation: every worker corpus row

Eight paired samples per ARM64 row and six paired samples per AMD64 row. `~`
means benchstat did not find a significant difference. The micro rows are
included for completeness; follow-up 200 ms runs found tiny/fib serial latency
flat and many-functions improved.

| Corpus/mode | ARM main | ARM branch | ARM delta | AMD main | AMD branch | AMD delta |
|---|---:|---:|---:|---:|---:|---:|
| tiny/p1 | 75.1 us | 74.5 us | ~ | 70.1 us | 84.8 us | noisy |
| tiny/p4 | 43.3 us | 42.2 us | ~ | 60.9 us | 57.2 us | ~ |
| tiny/p8 | 30.7 us | 28.6 us | ~ | 46.8 us | 46.6 us | ~ |
| fib_rec/p1 | 38.8 us | 31.4 us | -18.91% | 35.6 us | 36.5 us | ~ |
| fib_rec/p4 | 25.8 us | 27.1 us | ~ | 33.4 us | 30.6 us | ~ |
| fib_rec/p8 | 20.5 us | 19.5 us | ~ | 29.8 us | 33.5 us | ~ |
| many_funcs/p1 | 213.3 us | 229.4 us | ~ | 332.9 us | 335.6 us | ~ |
| many_funcs/p4 | 180.4 us | 170.0 us | -5.75% | 221.9 us | 217.5 us | ~ |
| many_funcs/p8 | 196.4 us | 174.5 us | ~ | 212.2 us | 213.4 us | ~ |
| json-as/p1 | 958.9 us | 924.4 us | ~ | 1.14 ms | 1.07 ms | -5.79% |
| json-as/p4 | 499.8 us | 369.0 us | -26.18% | 643.9 us | 524.3 us | -18.57% |
| json-as/p8 | 463.7 us | 348.5 us | -24.84% | 597.9 us | 539.4 us | ~ |
| blake-as/p1 | 262.6 us | 254.7 us | ~ | 310.5 us | 321.1 us | ~ |
| blake-as/p4 | 244.7 us | 215.9 us | -11.78% | 263.5 us | 259.0 us | ~ |
| blake-as/p8 | 237.4 us | 206.5 us | -13.02% | 255.4 us | 249.7 us | ~ |
| lua/p1 | 14.05 ms | 13.01 ms | -7.43% | 17.25 ms | 15.77 ms | -8.58% |
| lua/p4 | 5.95 ms | 4.12 ms | -30.67% | 7.74 ms | 5.28 ms | -31.73% |
| lua/p8 | 4.96 ms | 3.01 ms | -39.28% | 6.39 ms | 4.10 ms | -35.79% |
| sqlite3/p1 | 54.10 ms | 49.89 ms | -7.78% | 65.90 ms | 60.67 ms | -7.93% |
| sqlite3/p4 | 21.30 ms | 14.39 ms | -32.45% | 26.92 ms | 18.67 ms | -30.66% |
| sqlite3/p8 | 16.12 ms | 8.60 ms | -46.65% | 20.38 ms | 11.45 ms | -43.80% |
| ruby/p1 | 585.41 ms | 514.24 ms | -12.16% | 721.06 ms | 619.31 ms | -14.11% |
| ruby/p4 | 239.55 ms | 141.07 ms | -41.11% | 292.90 ms | 180.24 ms | -38.46% |
| ruby/p8 | 181.23 ms | 81.18 ms | -55.20% | 230.31 ms | 114.68 ms | -50.21% |
| esbuild/p1 | 373.83 ms | 341.88 ms | -8.55% | 435.54 ms | 391.33 ms | -10.15% |
| esbuild/p4 | 155.39 ms | 96.11 ms | -38.15% | 200.37 ms | 129.06 ms | -35.59% |
| esbuild/p8 | 125.38 ms | 60.28 ms | -51.92% | 148.09 ms | 67.67 ms | -54.31% |

## Public pipeline: all substantial corpus rows

| Corpus/mode | ARM main | ARM branch | ARM delta | AMD main | AMD branch | AMD delta |
|---|---:|---:|---:|---:|---:|---:|
| json-as/p1 | 1.69 ms | 1.38 ms | -17.98% | 1.92 ms | 1.61 ms | -16.18% |
| json-as/p4 | 1.05 ms | 640.5 us | -38.92% | 1.36 ms | 918.5 us | -32.23% |
| json-as/auto | 996.7 us | 594.4 us | -40.37% | 1.24 ms | 787.3 us | -36.38% |
| blake-as/p1 | 453.3 us | 361.0 us | -20.36% | 535.5 us | 441.4 us | -17.57% |
| blake-as/p4 | 412.7 us | 329.6 us | -20.13% | 477.1 us | 385.3 us | -19.24% |
| blake-as/auto | 418.8 us | 344.9 us | -17.65% | 517.3 us | 437.8 us | -15.37% |
| lua/p1 | 23.25 ms | 18.40 ms | -20.86% | 27.57 ms | 21.91 ms | -20.52% |
| lua/p4 | 12.89 ms | 6.67 ms | -48.25% | 15.87 ms | 8.39 ms | -47.15% |
| lua/auto | 12.63 ms | 6.47 ms | -48.76% | 15.79 ms | 7.98 ms | -49.47% |
| sqlite3/p1 | 87.57 ms | 69.89 ms | -20.19% | 105.06 ms | 84.03 ms | -20.02% |
| sqlite3/p4 | 45.51 ms | 23.11 ms | -49.23% | 56.14 ms | 29.19 ms | -48.00% |
| sqlite3/auto | 45.05 ms | 23.09 ms | -48.74% | 54.63 ms | 27.89 ms | -48.95% |
| ruby/p1 | 978.54 ms | 756.46 ms | -22.70% | 1.18 s | 906.03 ms | -22.97% |
| ruby/p4 | 516.11 ms | 248.63 ms | -51.83% | 623.25 ms | 307.22 ms | -50.71% |
| ruby/auto | 513.20 ms | 254.55 ms | -50.40% | 618.53 ms | 302.72 ms | -51.06% |
| esbuild/p1 | 640.70 ms | 511.90 ms | -20.10% | 744.87 ms | 589.19 ms | -20.90% |
| esbuild/p4 | 362.67 ms | 174.25 ms | -51.95% | 441.42 ms | 231.26 ms | -47.61% |
| esbuild/auto | 351.96 ms | 174.48 ms | -50.43% | 423.91 ms | 211.63 ms | -50.08% |

## Execution guard: complete executable corpus

Generated native bytes are unchanged across every compilation row. Execution
was nevertheless measured across all 36 exported workloads: ARM64 geomean
`+0.17%`, AMD64 geomean `-1.90%`. Most AMD64 rows were statistically flat;
ARM64 showed mixed positive/negative process-order noise despite identical JIT
code. No execution claim is made by this PR.

<details>
<summary>Per-export significance summary</summary>

`~` means no significant difference. These are included as a guard, not as an
optimization result.

| Export | ARM64 | AMD64 |
|---|---:|---:|
| tiny.add | +1.96% | ~ |
| fib_iter.fib | +6.98% | ~ |
| fib_rec.fib | +3.51% | ~ |
| arith.run | +6.98% | ~ |
| float.run | +9.51% | ~ |
| memory.sum | ~ | ~ |
| memory_tree.run | +0.86% | ~ |
| globals.accumulate | ~ | +0.19% |
| dispatch.apply | +4.91% | ~ |
| branches.classify | +4.09% | ~ |
| many_funcs.run | +5.66% | ~ |
| linked_list.sum | ~ | ~ |
| mandelbrot.render | +8.13% | +0.14% |
| sieve.count | ~ | ~ |
| nbody.step | +1.86% | ~ |
| spectralnorm.run | ~ | ~ |
| fannkuch.run | +4.48% | +0.12% |
| matmul.run | ~ | ~ |
| quicksort.sortN | +4.05% | ~ |
| crc32.hashN | ~ | ~ |
| sha256.hashN | ~ | ~ |
| raytrace.render | ~ | ~ |
| json-as.serializeN | ~ | +2.18% |
| json-as.deserializeN | ~ | +2.02% |
| blake-as.hashN | ~ | -0.28% |
| utf-as.convertN | ~ | ~ |
| xjb-mulhi.mulhi | ~ | ~ |
| xjb-mulhi.runN | ~ | ~ |
| swar-pack-parse.pack | ~ | ~ |
| swar-pack-parse.parse4 | +3.59% | ~ |
| swar-pack-parse.runN | ~ | ~ |
| json-as-simd.serializeN | ~ | +0.82% |
| json-as-simd.deserializeN | ~ | ~ |
| blake-as-simd.hashN | +4.85% | -0.29% |
| utf-as-simd.convertN | ~ | ~ |
| utf-as-simd.validateN | +1.31% | ~ |

</details>

## Validation

- `go test ./...` on native ARM64.
- `go test -race ./src/core/compiler/backend/railshot/arm64`.
- `go test -race ./src/core/compiler/backend/railshot/amd64` on `hub@hub`.
- Native AMD64 core, semantic corpus, spectest, regression, and Core 3 suites.
- Serial/parallel hint parity tests and deterministic lowest-index failure tests.
- Exact generated-code byte counts across every benchmarked mode and corpus.

The native AMD64 host's full CLI suite has unrelated environment failures from
its Go 1.22 toolchain and globally signed-tag configuration; all compiler/runtime
and semantic suites pass there.

## Rejected probes

- Heavy-first function scheduling: -0.04%, removed.
- `encoding/binary.AppendUint32` ARM64 emission: +2.73%, removed.
- Cursor-backed ARM64 encoder: +2.76%, removed.
- Function-wide hint hotness precomputation: +0.51%, removed; the later
  recursion-frame path-weight hoist measured -0.78% and is retained.
- Combined ARM64 inline planning: +2.22% and +1.13% allocations, removed.
- Branch-chain replacement for the dense immediate-free opcode table: +0.39%, removed.

## Review map

| Area | Primary files | Invariant |
|---|---|---|
| Validation facts | `wasm/validation_analysis.go`, `validate*.go` | one fixed-size summary per function; validation behavior unchanged |
| Admission reuse | `src/wago/api.go`, `feature_usage.go`, GC root files | consumers fall back when analysis is absent |
| Hint parallelism | backend `compile.go`, `hints.go` | serial/parallel summaries and error ordering match exactly |
| Type caches | backend `module_types.go` | only large modules retain dense immutable caches |
| AMD64 inline scan | `amd64/inline.go` | same target order and all-calls-inline decision |

## Latest native ARM64 checkpoint

The branch-head all-corpus rerun against `main` reports:

| Metric | Delta |
|---|---:|
| End-to-end compile latency | **-10.93%** |
| Backend compile latency | **-3.49%** |
| End-to-end compile heap | **+1.07%** |
| Backend compile heap | **+0.08%** |
| Generated ARM64 code size | **0.00%** on every module |
| Execution latency | **+0.19%** |

Lua, SQLite, Ruby, and esbuild improve 18.2-21.8% end to end. The complete
36-module compile table, 36-row execution table, absolute large-module heap,
method, retained commits, and rejected ARM64 probes are checked in as
[`REPORT.md`](./REPORT.md).
