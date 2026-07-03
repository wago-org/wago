# wago benchmarks

Kept in a **separate Go module** so the root package stays dependency-free. Two
complementary suites live here:

1. **Comparison** (`bench_test.go`) — `wago` versus
   [wazero](https://github.com/tetratelabs/wazero) v1.9 on a fixed set of
   programs. Charted by `./chart`.
2. **Stage suite** (`suite_test.go`) — wago-only, every pipeline **stage** across
   a curated **corpus** of modules (`corpus/`). This is what feeds the
   **perf-over-time** history. Published by `./cmd/benchpub`.

## Run

```bash
go test -bench . -benchmem                  # everything, raw numbers
go test -bench '^BenchmarkCompile$' -benchmem   # one stage across the corpus
go test -bench 'Decode|Exec' -benchmem      # a couple of stages
go test -bench . -benchmem -wago.bench.isa  # include generated ISA micro-suite
WAGO_BOUNDS=signals go test -tags wago_guardpage -bench '^BenchmarkExec/memory_tree\.run$' -benchmem

go run ./chart                              # wago-vs-wazero charts (gitignored)
go run ./cmd/benchpub -out out              # stage suite -> JSON + trend charts
go run ./cmd/benchpub -isa -out out         # include generated ISA micro-suite
go run ./cmd/validatestats -runs 30         # repeated validate wall-time stats
../scripts/update-website-bench.mjs         # refresh ../website performance copy
```

## Stage suite + corpus

`suite_test.go` benchmarks each module in `corpus/` through every stage, so
results read as `Stage/<module>`:

| Stage | Times |
|---|---|
| `Decode` | wasm bytes → byte-backed `*Module` (function locals + raw BodyBytes, no production function-body AST) |
| `Validate` | type-check a decoded module |
| `Compile` | native codegen for a decoded+validated module |
| `CompileFull` | end-to-end `wago.Compile` (decode+validate+compile) |
| `Instantiate` | instance setup for a compiled module |
| `Exec` | host→wasm call of each module's manifest entry point(s) |

The corpus (see `corpus/manifest.json`) spans three tiers, all with `.wasm`
checked in so the suite needs no toolchain at run time:

- **synthetic micros** — one codegen aspect each (micro / loop / calls /
  calls+memory / alu / fp / memory / globals / control / scale). Hand-written
  `.wat` in `corpus/src/` (plus the `many_funcs` generator); regenerate with
  `corpus/build.sh` (needs `wat2wasm`). The `calls+memory` fixture (`memory_tree`)
  is a recursive call tree that churns linear memory at every node, to expose
  regressions that only appear when internal calls and load/store traffic combine.
- **`compute` kernels** — real algorithms exercising several aspects together:
  `linked_list` (dependent-load pointer chase), `mandelbrot` (f64 escape-time),
  `sieve` (memory + strided marking + branches). Also `.wat` via `corpus/build.sh`.
- **`real` / `real-large` programs** — third-party code: the AssemblyScript
  libraries `json-as` (JSON serialize/deserialize), `blake-as` (BLAKE3 hash), and
  `utf-as` (UTF-8↔UTF-16 transcode), plus the `wasm3` interpreter (WASI, so
  `Decode`/`Validate` only — its host imports return values the backend can't
  compile yet). The AS modules are host-driven bench builds (`assembly/wago-bench.ts`
  in each library — an i32-count loop returning an i32 DCE sink); regenerate with
  `corpus/build-as.sh` (needs the AS libraries under `$AS_ROOT` + `asc`). wago has
  no start section, so they set the manifest's `init` to `_initialize` (the host
  calls it once after instantiate) and any host import (`env.abort`) is satisfied
  with a no-op stub.

- **`isa` micro-suite** — opt-in via `-wago.bench.isa`, `benchpub -isa`, or
  `make bench BENCH_ISA=1`. It has one exported function per *individual opcode* (i32/i64
  arithmetic·logic·shift·div·bitcount, f32/f64 arith·min/max·sqrt·rounding, memory
  load/store sequential+strided, control br_if/if_else/br_table/select, direct +
  indirect calls, local/global get·set, width/type conversions). Each isolates its
  opcode in a **coupled dual-accumulator dependent chain** (`a=a OP b; b=b OP a`)
  so there is no ILP, CSE, constant fold, or DCE to hide latency — the raw ns/op is
  directly comparable between opcodes and between engines. This is the tier for
  finding base-level per-primitive codegen gaps. Generated (`.wat` + a standalone
  `corpus/isa-manifest.json`) by `corpus/gen`; regenerate with `corpus/build.sh`.
  Compare a family across engines with e.g.
  `go test -run '^$' -bench 'Exec/isa_f64' -count 6 .` (wago) next to the matching
  `WazeroExec/isa_f64` rows.

A module's unsupported stages (via a `stages` list, or because the backend can't
compile it) are simply not benchmarked. Optional extra binaries can still be
dropped in via manifest `path` entries (skipped if absent; see `corpus/fetch.sh`).

## Cross-engine comparison

`compare_test.go` runs **wazero** (`CompileModule` + exec) over the same corpus,
and `benchpub -warp <harness>` shells out to **WARP**'s native harness for both
compile and exec. The harness is `vb_bench`, built from `warp/bench/main.cpp` —
since `warp/` is a submodule, the bench tweak (take real `i32` args, time a proper
exec loop with a DCE sink) lives as `bench/warp/bench-main.patch`.

From a fresh clone, `make bench-warp` does everything: it checks out the `warp/`
submodule (non-recursive — the x86-64 bench build needs none of WARP's own nested
submodules; the softfloat one is for the TriCore backend only), applies the patch
to a pristine harness, builds `vb_bench` with the fair-comparison config (eager
allocation on, interruption off), and runs it over the corpus. Only `cmake` and a
C++14 toolchain are required. `scripts/build-warp-bench.sh` is the build step on
its own. Two extra charts are produced:

- `compile-engines.svg` — compile time per module, wago vs wazero vs WARP. Where
  the backend can't compile a module yet, wago's **validate** time is shown
  (dimmed) so the big binaries still appear.
- `exec-engines.svg` — execution time per export, wago vs wazero vs WARP, on the
  real workloads (same manifest args for all three engines).

wazero compiles everything (including the WASI binaries), so the comparison shows
both where wago's single-pass compiler wins (small modules) and where its
byte-backed decode/validate path still dominates time on very large inputs.

## Perf over time

For focused validator work, `cmd/validatestats` measures repeated wall-clock
runs and reports average, median, and max duration for the validator path:

```bash
cd bench
go run ./cmd/validatestats -runs 30 -warmup 5         # full corpus
go run ./cmd/validatestats -file ../tests/testdata/fib.wasm
```

The measured path is the CLI-equivalent `wago validate <file>` flow:
byte-backed `DecodeModule` + `ValidateModule`. It does not build or verify IR.

`cmd/benchpub` runs the stage suite, records a **versioned** JSON run
(`git describe` + commit + date + cpu), appends it to a rolling `history.json`,
and renders per-stage **latency** (this run) and per-stage **trend** (across
versions). When real-world modules are enabled in the manifest (`compute` /
`real` / `real-large`), it also renders a dedicated **real-world** chart
(`realworld.svg`) comparing them side by side — one group per module (ordered by
wasm size), one bar per pipeline stage it reaches, so the big binaries that only
decode/validate read as gaps. With the default synthetic-only manifest that chart
is simply not produced. `scripts/publish-bench.sh` does this against the
[`wago-org/docs`](https://github.com/wago-org/docs) repo so the history
accumulates in `docs/bench/`:

```bash
./scripts/publish-bench.sh      # run + append history + push charts (stable machine)
```

`make bench-website` updates the sibling `../website` checkout's static
performance section from the latest `bench/.bench-run.txt`, runs the website
stats sync, and rebuilds its `dist/` directory. `make bench-publish` does the
same update automatically when `../website` exists.

## What's measured

| Benchmark | What it times |
|---|---|
| `Compile` | decode + validate + compile a module |
| `Instantiate` | set up an executable instance |
| `ExecCallOverhead` | host→wasm round trip (tiny function) |
| `ExecFibLoop` | iterative `fib(30)` |
| `ExecFibRec` | recursive `fib` (internal-call heavy) |
| `ExecGlobalGet` / `ExecGlobalSet` | exported-function access to a mutable global |
| `ExecLocalGet` / `ExecMemoryLoad` | context for globals versus local and memory access |

## Charts

`go run ./chart` renders SVG bar charts into `bench/charts/` (gitignored) — a
pure-Go, zero-dependency take on json-as's hand-built SVG charts (no Chart.js /
browser):

- `speedup.svg` — speedup vs wazero per benchmark (log scale; green = wago
  faster, red = slower)
- `latency.svg` — ns/op, wago vs wazero (grouped, log scale)

The published copies live in the [`wago-org/docs`](https://github.com/wago-org/docs)
repo under `charts/` and are embedded in the root README via raw URLs. Run
`./scripts/publish-charts.sh` (from the repo root) to regenerate on a stable
machine and push them there — benchmarks are never charted from CI, where shared
runners make the numbers noisy.
