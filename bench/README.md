# wago benchmarks

`wago` versus [wazero](https://github.com/tetratelabs/wazero) v1.9, kept in a
**separate Go module** so the root package stays dependency-free.

## Run

```bash
go test -bench . -benchmem      # raw numbers
go run ./chart                  # render charts into bench/charts/ (gitignored)
go run ./chart -in saved.txt    # ...or chart saved `go test -bench` output
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
