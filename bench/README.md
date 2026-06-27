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

The corpus spans micro / loop / calls / alu / fp / memory / globals / control /
scale categories (see `corpus/manifest.json`). The synthetic `.wasm` files are
checked in for stability; regenerate them from the `.wat` sources (and the
`many_funcs` / `big_func` generators) with `corpus/build.sh` (needs `wat2wasm`).

It also includes **real-world binaries** referenced in place (via a manifest
`path`, skipped if absent): the `*stack` family (7 KB–235 KB, full pipeline) and
larger modules — a DWARF library (~428 KB) and PSPDFKit (~9 MB) — that the
backend cannot yet compile, so a `stages` list limits them to `Decode`/`Validate`
(where a 9 MB module is a useful stress: decode currently allocates ~700 MB).
A module's missing stages are simply not benchmarked.

## Perf over time

`cmd/benchpub` runs the stage suite, records a **versioned** JSON run
(`git describe` + commit + date + cpu), appends it to a rolling `history.json`,
and renders per-stage **latency** (this run) and **trend** (across versions)
charts. `scripts/publish-bench.sh` does this against the
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
