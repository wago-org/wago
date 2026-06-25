# wago benchmarks

`wago` versus [wazero](https://github.com/tetratelabs/wazero) v1.9, kept in a
**separate Go module** so the root package stays dependency-free.

## Run

```bash
go test -bench . -benchmem      # raw numbers
go run ./chart                  # re-render charts/*.svg from a fresh run
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

## Charts

`go run ./chart` renders SVG bar charts into `charts/` — a pure-Go, zero-dependency
take on json-as's hand-built SVG charts (no Chart.js / browser):

- `charts/speedup.svg` — speedup vs wazero per benchmark (log scale; green = wago
  faster, red = slower)
- `charts/latency.svg` — ns/op, wago vs wazero (grouped, log scale)
