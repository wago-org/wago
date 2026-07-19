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

A shared-state audit classifies the worker-visible module context as follows:

- decoded module metadata, direct byte-backed metadata, and declared-function
  bits are immutable after serial module validation;
- each worker's operand/control stacks, byte reader, result slot, locals, and
  decoded-immediate scratch are worker-local;
- the resolved component-type cache is the one lookup structure that is mutable
  while module validation runs, so every valid flat index is resolved and the
  cache is frozen before workers start; malformed misses are computed without
  insertion;
- `moduleValidator.constFV` is mutable serial scratch for module-level constant
  expressions and is not reachable from function-body validation.

The final point required a correction during review. `table.init` originally
called the full element-payload validator from each function body. For expression-
based segments that redundantly revalidated initializer bytes through the shared
`constFV`, racing its stacks, reader, result slot, and decode scratch; the race
reproduced under `go test -race` and could panic in `popCtrl`. Element expressions
already validate serially before body workers start, so `table.init` now performs
a read-only element reference-type lookup: `ElemFuncs` and `ElemFuncExprs` map to
`funcref`, while `ElemTypedExprs` uses its declared reference type. The direct
byte-backed path performs the equivalent lookup from `directElem`. Defensive
index and unknown-kind errors remain, without revisiting initializer expressions.

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

### `table.init` race-fix regression check

The required p1/p4 matrix was repeated immediately before and after the read-only
element-type fix with `GOMAXPROCS=8`, `-benchtime=500ms`, `-count=5`, and
`-benchmem`. Values below are medians. Latency changes are within run noise (the
pre-fix Ruby p1 samples ranged from 228.5-255.4 ms); serial B/op and allocs/op are
exactly unchanged on every module. Parallel allocation counts are also unchanged;
minor p4 B/op variation reflects worker scheduling and retained stack capacity.

| module | workers | before | after | latency delta | B/op before / after | allocs/op before / after |
|---|---:|---:|---:|---:|---:|---:|
| many_funcs | 1 | 47.018 us | 46.376 us | -1.4% | 1,777 / 1,777 | 11 / 11 |
| many_funcs | 4 | 30.375 us | 30.860 us | +1.6% | 9,680 / 9,680 | 21 / 21 |
| json-as | 1 | 0.309 ms | 0.312 ms | +0.9% | 10,208 / 10,208 | 50 / 50 |
| json-as | 4 | 0.122 ms | 0.123 ms | +0.9% | 20,834 / 20,828 | 75 / 75 |
| lua | 1 | 4.714 ms | 4.701 ms | -0.3% | 78,944 / 78,944 | 382 / 382 |
| lua | 4 | 1.453 ms | 1.456 ms | +0.2% | 114,413 / 114,448 | 415 / 415 |
| sqlite3 | 1 | 17.709 ms | 17.648 ms | -0.3% | 189,072 / 189,072 | 649 / 649 |
| sqlite3 | 4 | 4.909 ms | 4.857 ms | -1.1% | 317,118 / 317,372 | 687 / 687 |
| ruby | 1 | 244.858 ms | 222.880 ms | -9.0% | 447,248 / 447,248 | 3,938 / 3,938 |
| ruby | 4 | 62.955 ms | 62.612 ms | -0.5% | 994,412 / 994,412 | 3,982 / 3,982 |
| esbuild | 1 | 152.559 ms | 150.263 ms | -1.5% | 2,080,360 / 2,080,360 | 4,117 / 4,117 |
| esbuild | 4 | 46.893 ms | 45.659 ms | -2.6% | 4,109,376 / 4,154,165 | 4,173 / 4,173 |

No meaningful regression was observed, and removing repeated element-expression
validation makes `table.init` body checking strictly less work.

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
- concurrent `table.init` validation over raw `ref.null` / `ref.func`
  expression-based elements in both materialized and direct byte-backed forms;
- exact serial/p2/p4/p8 parity for valid and table/element-type-invalid modules;
- module-phase initializer rejection plus structural checks that the function
  phase does not revisit initializer expressions;
- the explicit decoded-byte-backed validation API;
- public config plus `wago run`/`wago validate` CLI plumbing using the same
  worker policy for validation and codegen;
- race testing of the wasm validator and public compile packages.

The `table.init` correction was verified with the focused tests at race-detector
count 20, the complete validator/config/CLI race set, `make test`, both explicit
and guard-page `make test-corpus` runs, `go vet ./...`, Go 1.22 Staticcheck
2024.1.1 through mise, and 20 repetitions of the existing worker-validation test
set. All completed successfully.

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
go test -race ./src/core/compiler/wasm \
  -run 'TestValidate.*TableInit.*' -count=20
go test -race ./src/core/compiler/wasm ./src/wago
```
