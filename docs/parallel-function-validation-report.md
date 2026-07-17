# Parallel function validation experiment

Date: 2026-07-17

Branch: `experiment/parallel-function-validation`

Baseline commit: `145aec3471792d8307a7f91288b38f86dd9ed730`

## Decision

Use the existing opt-in function-worker policy for validation as well as native
code generation. `WithFunctionWorkers(1)` and the default configuration retain
the serial validator. `WithFunctionWorkers(0)` / `wago run -p` use the adaptive
module-size policy, and forced values such as `-p4` or `-p8` apply the same
bounded worker maximum to validation and codegen. The earlier
`WithCompileWorkers` name remains as a deprecated source-compatible alias.

The existing adaptive threshold remains appropriate:

```text
score = total wasm function-body bytes + 64 * local function count
score < 16 KiB: serial
otherwise:       at most 4 workers
always cap by GOMAXPROCS and local function count
```

Forced parallelism is intentionally not profitable for tiny modules: the tiny
corpus module grows from about 0.82 us serial to 3.28 us with workers. Adaptive
mode keeps that module serial. At four workers, validation itself improves 58-72%
on the representative medium/large modules and 34% on the 301-function
`many_funcs` scale fixture.

## Environment and method

- Go 1.24.4, linux/amd64
- AMD Ryzen 7 8845HS, 8 physical / 16 logical cores
- `GOMAXPROCS=8`
- baseline and implementation from the same checkout and corpus
- validation matrix: `-benchtime=500ms -count=8 -benchmem`
- full public pipeline: `-benchtime=3x -count=6 -benchmem`
- reported values are medians

Raw captures for this experiment were written to
`.validation/parallel-validation-2026-07-17/` and are intentionally not tracked.

## Implementation

Module-level validation remains serial. It validates types, imports, globals,
constant expressions, elements, data, exports, and the start declaration before
any worker starts. Function bodies then use a bounded worker pool:

- `workers <= 1` uses the original reusable-validator serial path;
- worker counts are capped by the local-function count (the public config also
  caps them by `GOMAXPROCS`);
- each worker owns one `funcValidator`, including its operand/control stacks,
  byte reader, and decoded-immediate scratch;
- workers claim function indexes from an atomic counter, which balances modules
  containing a mixture of tiny wrappers and very large functions;
- one result slot is owned by each function index;
- errors are scanned in original function order after the join, preserving the
  serial validator's lowest-function-index result regardless of scheduling;
- the parallel helper is separate from the serial path, so goroutine closure and
  worker bookkeeping do not escape into serial validation.

The module validator's resolved component-type cache was the one shared mutable
object on the body-validation path. Before workers start, every valid flat type
index is resolved and the cache is frozen. Workers then perform concurrent
read-only lookups. A malformed body that names an invalid type index computes the
miss without inserting it, keeping invalid-module validation race-free.

The low-level worker-aware entry points are:

- `wasm.ValidateModuleWithWorkers`;
- `wasm.ValidateByteBackedModuleWithWorkers`;
- `wasm.ValidateDecodedByteBackedModuleWithWorkers`.

The existing serial entry points delegate with one worker.

## Validation latency

| module | baseline serial | p1 after | p2 | p4 | p8 | p4 vs baseline | p8 vs baseline |
|---|---:|---:|---:|---:|---:|---:|---:|
| tiny | 0.816 us | 0.818 us | 3.279 us | 3.276 us | 3.296 us | +301.4% | +303.9% |
| many_funcs | 46.984 us | 48.018 us | 39.979 us | 30.929 us | 34.172 us | -34.2% | -27.3% |
| json-as | 0.312 ms | 0.314 ms | 0.184 ms | 0.130 ms | 0.120 ms | -58.2% | -61.6% |
| lua | 4.771 ms | 4.897 ms | 2.555 ms | 1.541 ms | 1.197 ms | -67.7% | -74.9% |
| sqlite3 | 17.778 ms | 18.246 ms | 9.360 ms | 5.222 ms | 3.433 ms | -70.6% | -80.7% |
| ruby | 231.500 ms | 234.055 ms | 118.994 ms | 64.702 ms | 42.981 ms | -72.1% | -81.4% |
| esbuild | 152.371 ms | 156.582 ms | 83.583 ms | 48.214 ms | 34.470 ms | -68.4% | -77.4% |

The baseline/p1 latency differences are run-to-run noise at these sample counts;
serial allocation counts are exactly unchanged for every row.

## Allocation cost

| module | serial B/op | p4 B/op | p8 B/op | serial allocs/op | p4 allocs/op | p8 allocs/op |
|---|---:|---:|---:|---:|---:|---:|
| tiny | 1,794 | 2,912 | 2,912 | 12 | 18 | 18 |
| many_funcs | 1,777 | 9,680 | 13,584 | 11 | 21 | 29 |
| json-as | 10,208 | 20,812 | 31,178 | 50 | 75 | 100 |
| lua | 78,944 | 114,510 | 141,246 | 382 | 415 | 450 |
| sqlite3 | 189,072 | 317,294 | 360,044 | 649 | 687 | 729 |
| ruby | 447,248 | 966,139 | 1,157,548 | 3,938 | 3,982 | 4,032 |
| esbuild | 2,080,360 | 4,094,304 | 5,620,550 | 4,117 | 4,173 | 4,236 |

The absolute worker overhead is bounded by the worker count and stack capacity
needed by the functions each worker encounters. It is a good latency trade for
explicit/adaptive compile-once workloads, but it reinforces keeping serial as
the library default and four workers as the adaptive cap.

## Full public compile pipeline

`BenchmarkCompileFullWorkers` includes decode, validation, frontend support
checks, metadata construction, and codegen when codegen is not link-deferred.
The table compares p4 before this change (parallel codegen only) with p4 after
this change (parallel validation plus codegen).

| module | old p4 | new p4 | improvement from validation | new p4 vs new p1 |
|---|---:|---:|---:|---:|
| many_funcs | 0.342 ms | 0.314 ms | -8.2% | -33.0% |
| json-as | 1.610 ms | 1.318 ms | -18.1% | -33.0% |
| lua | 15.009 ms | 12.287 ms | -18.1% | -22.9% |
| sqlite3 | 50.361 ms | 38.841 ms | -22.9% | -27.0% |
| ruby | 684.196 ms | 518.094 ms | -24.3% | -28.8% |
| esbuild | 631.591 ms | 513.108 ms | -18.8% | -42.4% |

`lua`, `sqlite3`, and `ruby` defer backend codegen because of returning imports.
They previously received no initial-compile benefit from `-p`; parallel validation
now reduces their public compile latency without changing link behavior.

## Correctness properties and tests

Targeted tests cover:

- successful serial/p2/p4/p8 parity on a 256-function byte-backed module;
- serial/p2/p4/p8 parity across tiny, many-functions, scalar/SIMD real-world,
  interpreter, database, Ruby, and esbuild corpus modules;
- worker counts above the function count;
- deterministic lowest-function-index errors over repeated parallel runs;
- the explicit decoded-byte-backed validation API;
- public config plus `wago run`/`wago validate` CLI plumbing using the same
  worker policy for validation and codegen;
- race testing of the wasm validator and public compile packages.

## Reproduction

Validation worker matrix:

```sh
cd bench
GOMAXPROCS=8 go test . -run '^$' \
  -bench '^BenchmarkValidateWorkers/(tiny|many_funcs|json-as|lua|sqlite3|ruby|esbuild)/(p1|p2|p4|p8)$' \
  -benchmem -count=8 -benchtime=500ms
```

Full public pipeline:

```sh
cd bench
GOMAXPROCS=8 go test . -run '^$' \
  -bench '^BenchmarkCompileFullWorkers/(many_funcs|json-as|lua|sqlite3|ruby|esbuild)/(p1|p4)$' \
  -benchmem -count=6 -benchtime=3x
```

Correctness and race checks:

```sh
go test ./src/core/compiler/wasm ./src/wago ./cli/wagocli
go test -race ./src/core/compiler/wasm ./src/wago
```
