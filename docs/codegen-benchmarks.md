# Codegen benchmarks and profiling

Focused compiler-path benchmarks cover byte-backed frontend decode/validate,
railshot backend compilation, and decode/validate/compile end-to-end paths.

Run the main benchmark suites with allocation reporting:

```sh
go test ./src/core/compiler/backend/railshot -bench=. -benchmem
go test ./src/core/compiler/frontend -bench=. -benchmem
go test ./src/core/compiler/wasm -bench=. -benchmem
```

Capture railshot profiles:

```sh
go test ./src/core/compiler/backend/railshot -bench=BenchmarkRailshotCompile -benchmem -cpuprofile cpu.out -memprofile mem.out
go tool pprof -top mem.out
go tool pprof -top cpu.out
```

Capture frontend profiles:

```sh
go test ./src/core/compiler/frontend -bench=. -benchmem -cpuprofile cpu.out -memprofile mem.out
go tool pprof -top mem.out
go tool pprof -top cpu.out
```

Benchmarks should keep setup outside the timed loop, call `b.ReportAllocs()`,
and store compiled or decoded results in package-level sinks so the compiler
cannot eliminate the work.
