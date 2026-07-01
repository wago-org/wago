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
WAGO_X64=1 WAGO_BOUNDS=signals go test -tags wago_guardpage -bench '^BenchmarkExec/memory_tree\.run$' -benchmem

go run ./chart                              # wago-vs-wazero charts (gitignored)
go run ./cmd/benchpub -out out              # stage suite -> JSON + trend charts
```

## Stage suite + corpus

`suite_test.go` benchmarks each module in `corpus/` through every stage, so
results read as `Stage/<module>`:

| Stage | Times |
|---|---|
| `Decode` | wasm bytes → `*Module` |
| `Validate` | type-check a decoded module |
| `Compile` | native codegen for a decoded+validated module |
| `CompileFull` | end-to-end `wago.Compile` (decode+validate+compile) |
| `Instantiate` | instance setup for a compiled module |
| `Exec` | host→wasm call of each module's manifest entry point(s) |

The default corpus is a small set of synthetic modules spanning micro / loop /
calls / calls+memory / alu / fp / memory / globals / control / scale categories
(see `corpus/manifest.json`) — kept deliberately small so the suite runs quickly.
The `calls+memory` fixture (`memory_tree`) is a recursive call tree that churns
linear memory at every node, intended to expose regressions that only appear when
internal calls and load/store traffic combine. The `.wasm` files are checked in
for stability; regenerate them from the `.wat` sources (and the `many_funcs`
generator) with `corpus/build.sh` (needs `wat2wasm`).

The suite can also exercise heavier, real-world modules. They are **supported but
not enabled in the default manifest** — add `path` entries to turn them on:

- **vendored binaries** (`corpus/fetch.sh`, downloaded into the gitignored
  `corpus/vendor/`, skipped if absent): e.g. the wasm3 interpreter built for WASI,
  or a `clang.wasm` (drop one in `corpus/vendor/clang.wasm` or set
  `CLANG_WASM_URL`). These validate on wago but the backend can't compile them yet
  (WASI imports), so they'd be `Decode`/`Validate` only.
- **`real` / `real-large`** third-party binaries referenced in place (manifest
  `path`, skipped if absent): the `*stack` family (7 KB–235 KB, full pipeline) and
  a DWARF library (~428 KB) plus PSPDFKit (~9 MB) that the backend cannot compile
  yet, so a `stages` list limits them to `Decode`/`Validate` (decoding the 9 MB
  module currently allocates ~700 MB — a useful stress).

A module's unsupported stages are simply not benchmarked.

## Cross-engine comparison

`compare_test.go` runs **wazero** (`CompileModule` + exec) over the same corpus,
and `benchpub -warp <harness>` shells out to **WARP**'s native harness for both
compile and exec. The harness is `vb_bench`, built from `warp/bench/main.cpp` —
since `warp/` is a submodule, the bench tweak (take real `i32` args, time a proper
exec loop with a DCE sink) lives as `bench/warp/bench-main.patch`; build it with
`./scripts/build-warp-bench.sh`. Two extra charts are produced:

- `compile-engines.svg` — compile time per module, wago vs wazero vs WARP. Where
  the backend can't compile a module yet, wago's **validate** time is shown
  (dimmed) so the big binaries still appear.
- `exec-engines.svg` — execution time per export, wago vs wazero vs WARP, on the
  real workloads (same manifest args for all three engines).

wazero compiles everything (including the WASI binaries), so the comparison shows
both where wago's single-pass compiler wins (small modules) and where its
allocation-heavy decode/validate lags on very large inputs.

## Perf over time

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
