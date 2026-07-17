# Parallel function compilation experiment

Date: 2026-07-17

Branch: `parallel-func-comp`

Baseline commit: `e774b93849df0fd66d0c4ad8def0c73a8ed7ec82`

## Decision

**Ship the bounded worker implementation and public configuration, but keep the
default serial.** Applications that prioritize one-module compile latency can opt
into the measured adaptive policy with `WithCompileWorkers(0)` or force a maximum
with `WithCompileWorkers(N)`. The defaults used by `NewRuntimeConfig`,
`Compile(nil, ...)`, and `Load` when given raw wasm remain serial.

Enabling parallel compilation globally by default is not justified:

- single-module backend latency improves substantially on medium and large modules;
- end-to-end latency improves where backend codegen is a material part of the public
  pipeline;
- allocations increase by roughly 43–93% at four workers on representative modules;
- compile-once peak RSS for esbuild rises from about 147 MiB to 190 MiB at four
  workers;
- independent-module throughput does not reliably improve, and esbuild throughput
  regressed 16.5% under the adaptive four-worker policy at `GOMAXPROCS=8`;
- eight workers improve backend-only latency further on the largest modules, but
  add enough allocation/CPU/RSS cost that they are not suitable for auto mode.

The adaptive policy is therefore deliberately conservative:

```text
score = total wasm function-body bytes + 64 * local function count
score < 16 KiB: serial
otherwise:       at most 4 workers
always cap by GOMAXPROCS and local function count
```

Function count alone is insufficient: `many_funcs` has 301 functions but only
2,053 body bytes, while esbuild has 4,148 functions and 8,471,854 body bytes.

## Environment

- Go 1.24.4, linux/amd64
- AMD Ryzen 7 8845HS, 8 physical / 16 logical cores
- initial `GOMAXPROCS=16`; worker matrices explicitly used 1, 2, 4, and 8
- no `WAGO_*` environment overrides
- raw run directory:
  `/tmp/wago-parallel-func-comp-20260717T151140Z/`

Generated logs and profiles remain under `/tmp`, consistent with
`docs/codegen-benchmarks.md`. These paths are author-local and are not committed;
the reproduction commands below regenerate equivalent data. The important final
files are listed in [Raw data](#raw-data).

## Implementation

The amd64 and arm64 direct backends now accept `CompileOptions.Workers`.

- `Workers <= 1` uses a distinct serial path with the original allocation shape:
  one reusable scratch object, no worker metadata, atomics, goroutines, channels,
  or intermediate code arena.
- `Workers > 1` is capped by `GOMAXPROCS` and local-function count.
- Module-wide analyses and decisions remain serial and complete before workers
  start.
- Each worker owns one compiler scratch object and one append-only machine-code
  arena.
- A fixed-size pool takes function indexes from an atomic counter.
- Results retain arena offsets, relocation metadata, internal-entry offsets, and
  deterministic per-function stats destinations.
- Final code is joined in original function order, aligned identically, and then
  relocated using the original ordered layout.
- Errors are selected by the lowest function index, not completion order.
- The parallel implementation is in a separate helper so its goroutine closure
  does not escape into or add allocations to the serial path.

Parallel testing exposed pre-existing output nondeterminism in loop bounds-hoist
candidate ordering. Both backends now sort candidates by base local with the
generic `slices.SortFunc`, avoiding the allocation that `sort.Slice` would add to
the serial path.

Public integration:

- `WithCompileWorkers(0)`: adaptive, opt-in;
- `WithCompileWorkers(1)`: serial;
- `WithCompileWorkers(N)`, `N > 1`: forced maximum;
- `wago run -p`: adaptive CLI mode;
- `wago run -p8`, `wago run -p 8`, or `wago run --parallel=8`: forced CLI maximum;
- negative values fail `RuntimeConfig.Validate`;
- initial compilation and link-time recompilation use the same policy;
- the link policy is retained in a `uint16` non-serialized metadata slot, preserving
  the existing 632-byte `Compiled` footprint;
- serialized output is independent of worker policy, and deserialization does not
  restore process-local compile policy.

## Corpus shape

| module | local funcs | body bytes | maximum body |
|---|---:|---:|---:|
| tiny | 2 | 9 | 6 |
| fib_rec | 1 | 28 | 28 |
| many_funcs | 301 | 2,053 | 17 |
| json-as | 43 | 16,651 | 2,445 |
| blake-as | 6 | 4,474 | 3,532 |
| lua | 658 | 234,408 | 22,018 |
| sqlite3 | 2,831 | 798,392 | 39,188 |
| ruby | 17,452 | 11,143,998 | 100,238 |
| esbuild | 4,148 | 8,471,854 | 216,489 |

## Backend latency

Decode and validation were outside the timed loop. Values are medians from eight
runs at `-benchtime=3x`; `p4`/`p8` are effective maxima after the `GOMAXPROCS`
cap. The complete distributions and benchstat confidence tests are in the raw
files.

### `GOMAXPROCS=4`

| module | p1 | p2 | p4 | p8 | best versus p1 |
|---|---:|---:|---:|---:|---:|
| tiny | 5.6 us | 19.0 us | 15.9 us | 16.2 us | serial |
| fib_rec | 7.0 us | 6.8 us | 6.8 us | 6.3 us | noisy / effectively serial work |
| many_funcs | 255.6 us | 218.8 us | 167.6 us | 152.6 us | -40.3% |
| json-as | 1.006 ms | 641.4 us | 439.9 us | 395.2 us | -60.7% |
| blake-as | 209.1 us | 169.8 us | 169.2 us | 190.2 us | -19.1% at p4 |
| lua | 18.181 ms | 11.215 ms | 9.048 ms | 8.994 ms | -50.5% |
| sqlite3 | 56.445 ms | 34.353 ms | 22.718 ms | 22.567 ms | -60.0% |
| ruby | 611.153 ms | 382.929 ms | 257.990 ms | 248.788 ms | -59.3% |
| esbuild | 416.895 ms | 259.706 ms | 175.881 ms | 183.189 ms | -57.8% at p4 |

At `GOMAXPROCS=8`, p8 reached -64.6% on sqlite3, -68.9% on ruby, and
-66.1% on esbuild. This is a backend-only result; the full-pipeline and memory
results below are why auto mode stops at four.

## Full public pipeline

`BenchmarkCompileFullWorkers` includes decode, validation, frontend checks, and
backend codegen when codegen is not link-deferred. At `GOMAXPROCS=8`, adaptive
versus serial medians were:

| module | serial | adaptive | change | allocation ratio |
|---|---:|---:|---:|---:|
| many_funcs | 0.474 ms | 0.357 ms | -24.6% | 1.57x |
| json-as | 2.072 ms | 1.629 ms | -21.4% | 1.74x |
| esbuild | 878.327 ms | 632.513 ms | -28.0% | 1.44x |

`lua`, `sqlite3`, and `ruby` have returning imports, so public `Compile` defers
backend codegen until linking; their compile-only full-pipeline rows correctly
show no worker benefit. A separate generated returning-import benchmark forces
fresh link-time codegen each iteration. At `GOMAXPROCS=8`, adaptive link-pipeline
latency improved 8.6% (54.26 ms to 49.60 ms), while allocations rose 70.6%.
Forced p8 improved 14.4% but raised allocations 76.0%.

## Allocation, CPU, and RSS cost

At `GOMAXPROCS=8`, backend allocation changes for p4 versus p1 were:

| module | p1 B/op | p4 B/op | change |
|---|---:|---:|---:|
| many_funcs | 182.0 KiB | 305.9 KiB | +68.0% |
| json-as | 362.6 KiB | 701.5 KiB | +93.4% |
| lua | 5.934 MiB | 9.339 MiB | +57.4% |
| sqlite3 | 15.88 MiB | 26.98 MiB | +69.8% |
| ruby | 103.3 MiB | 174.5 MiB | +69.0% |
| esbuild | 97.78 MiB | 161.34 MiB | +65.0% |

Compile-once esbuild measurements used a fresh benchmark process per sample and
Python `resource.getrusage(RUSAGE_CHILDREN)`; values below are medians of five
runs at `GOMAXPROCS=8`.

| mode | backend latency | child user CPU | peak RSS |
|---|---:|---:|---:|
| p1 | 481.7 ms | 0.974 s | 147.4 MiB |
| p2 | 292.1 ms | 0.987 s | 176.9 MiB |
| p4 | 190.2 ms | 1.037 s | 190.4 MiB |
| p8 | 161.6 ms | 1.173 s | 189.9 MiB |

The p8 RSS median happened to be slightly below p4 in this five-process sample,
but p8 allocated 178.83 MiB/op versus 161.34 MiB/op for p4 in the repeated
backend benchmark and consumed more user CPU. It is not a lower-footprint mode.

CPU profiles confirm useful parallelism rather than one new serialized hotspot:
five esbuild backend compiles took 480.7 ms/op at p1 and 162.8 ms/op at p8, while
total sampled CPU increased from 3.60 s over 3.71 s wall time to 4.87 s over
1.74 s wall time. The same parsing, bounds-hoist, instruction classification, and
encoder routines remain dominant.

## Oversubscription

`BenchmarkCompileMultiModuleThroughput` uses `b.RunParallel` and is explicitly a
throughput benchmark, not a one-module latency benchmark. Adaptive versus p1:

| `GOMAXPROCS` | many_funcs | json-as | esbuild |
|---:|---:|---:|---:|
| 2 | no significant change | no significant change | +1.0% slower |
| 4 | no significant change | +6.5% slower | +2.2% slower |
| 8 | -7.7% faster | -6.0% faster | **+16.5% slower** |

The mixed result means a library cannot infer whether the caller values one-module
latency or aggregate server throughput. Keeping serial as the default is the
predictable choice; concurrent hosts can leave it serial, while CLI/build-style
callers can explicitly request adaptive compilation.

## Correctness and validation

Completed gates:

- byte-for-byte p1/p2/p4/p8 equality for code, `Entry`, `InternalEntry`, lengths,
  relocations, and per-function stats across repeated targeted modules;
- whole selected corpus parity including lua, sqlite3, ruby, and esbuild;
- deterministic lowest-index error behavior by construction;
- link-time serial/parallel output equality and policy propagation;
- serialized p1/p8 artifact equality and non-serialization of policy;
- targeted race runs for backend corpus parity and public/link paths;
- `make test`;
- `make test-corpus` in explicit and guard-page builds;
- native Linux/arm64 and Darwin/arm64 CI, including TinyGo on both hosts;
- arm64 backend and public-package cross-compilation before CI;
- WebAssembly 1.0: 629 modules and 16,026 assertions passed;
- WebAssembly 2.0: 1,600 modules and 48,248 assertions passed;
- WebAssembly 3.0 proposal run completed with zero failures; only the repository's
  documented unsupported/toolchain gaps were skipped. Exact skip counts vary with
  the proposal corpus/toolchain snapshot, so the hosted CI summary is authoritative.

The first `make test` run caught a 16-byte `Compiled` footprint increase from an
`int` policy field. The field was changed to a capped `uint16` placed in existing
padding; the 632-byte footprint tests now pass.

A side-by-side interleaved baseline/current serial run found no statistically
significant change for tiny, many_funcs, json-as, blake-as, sqlite3, ruby, or
esbuild. Lua measured +3.1% in that noisy `3x` sample. Serial allocation counts
are restored exactly (for example tiny remains 35 allocs/op), and the serial code
path has no parallel helper closure or worker bookkeeping.

## Reproduction

Representative backend matrix:

```sh
cd bench
for gp in 1 2 4 8; do
  GOMAXPROCS=$gp go test -run '^$' \
    -bench '^BenchmarkCompileWorkers/(tiny|fib_rec|many_funcs|json-as|blake-as|lua|sqlite3|ruby|esbuild)/' \
    -benchtime=3x -count=8 -benchmem -timeout=0 . \
    > /tmp/final-workers-all-gomaxprocs${gp}.txt
done
```

Public full pipeline:

```sh
GOMAXPROCS=8 go test -run '^$' \
  -bench '^BenchmarkCompileFullWorkers/.*/(p1|auto)$' \
  -benchtime=3x -count=8 -benchmem -timeout=0 ./bench
```

Link-time path:

```sh
GOMAXPROCS=8 go test ./src/wago -run '^$' \
  -bench '^BenchmarkCompileLinkWorkers/' \
  -benchtime=3x -count=8 -benchmem -timeout=0
```

Race and correctness:

```sh
go test -race ./src/core/compiler/backend/railshot/amd64 \
  -run 'TestCompileWorkers(Deterministic|CorpusParity)' -count=1 -timeout=0
go test -race ./src/wago \
  -run 'TestCompileWorkers(LinkPathAndSerialization|ForModulePolicy)' -count=1
make test
make test-corpus
PATH="$PWD/.tools/wabt-1.0.41-linux-x64/bin:$PATH" make spec
```

## Raw data

All raw data is under:

```text
/tmp/wago-parallel-func-comp-20260717T151140Z/
```

Key files:

- `environment.txt`, `module-metrics.txt`
- `baseline-backend-small.txt`, `baseline-compile-large.txt`
- `final-workers-all-gomaxprocs{1,2,4,8}.txt`
- `final-benchstat-workers-gomaxprocs{1,2,4,8}.txt`
- `full-auto-all-gomaxprocs{1,2,4,8}.txt`
- `benchstat-full-auto-gomaxprocs{1,2,4,8}.txt`
- `final-link-workers-gomaxprocs{1,2,4,8}.txt`
- `final-benchstat-link-workers-gomaxprocs{1,2,4,8}.txt`
- `oversub-small-gomaxprocs{2,4,8}.txt`
- `oversub-esbuild-auto4-gomaxprocs{2,4,8}.txt`
- `benchstat-oversub-gomaxprocs{2,4,8}.txt`
- `benchstat-oversub-esbuild-auto4-gomaxprocs{2,4,8}.txt`
- `final-compile-once-rss-esbuild.txt`
- `final-compile-once-rss-esbuild-summary.txt`
- `final-cpu-backend-esbuild-p{1,8}.pprof`
- `final-mem-backend-esbuild-p{1,8}.pprof`
- `final-profile-backend-esbuild-p{1,8}-top.txt`
- `final-profile-backend-esbuild-p{1,8}-alloc-top.txt`
- `final-benchstat-interleaved-base-vs-serial.txt`
- `final-race-backend-workers.txt`, `final-race-public-workers.txt`
- `final-make-test-complete.txt`, `final-test-corpus.txt`, `final-spec.txt`
- `raw-manifest.txt`, `SHA256SUMS.txt`
