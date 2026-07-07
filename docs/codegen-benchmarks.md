# Codegen benchmarks and profiling

Focused compiler-path benchmarks cover byte-backed frontend decode/validate,
railshot backend compilation, and decode/validate/compile end-to-end paths.
Generated benchmark logs and profiles such as `cpu.out`, `mem.out`,
`/tmp/railshot-before.txt`, and `/tmp/frontend_cpu.out` are local artifacts;
do not commit them. Prefer `/tmp/...` paths in examples so profiling does not
clutter the repository root.

## Stable benchmark runs

Use repeated runs when comparing performance:

```sh
go test ./src/core/compiler/backend/railshot -bench=. -benchmem -count=10 > /tmp/railshot-before.txt
go test ./src/core/compiler/frontend -bench=. -benchmem -count=10 > /tmp/frontend-before.txt
go test ./src/core/compiler/wasm -bench=. -benchmem -count=10 > /tmp/wasm-before.txt
```

Optional `benchstat` workflow:

```sh
go test ./src/core/compiler/backend/railshot -bench=. -benchmem -count=10 > /tmp/railshot-before.txt
# make changes
go test ./src/core/compiler/backend/railshot -bench=. -benchmem -count=10 > /tmp/railshot-after.txt
benchstat /tmp/railshot-before.txt /tmp/railshot-after.txt
```

## Backend toggles

Railshot inlines small leaf wasm functions by default. This is part of the
normal execution configuration because real AssemblyScript rules often rotate
through tiny string and range helpers where the call sequence is a large part of
the cost.

For A/B runs, disable the transform explicitly:

```sh
WAGO_INLINE=0 go test ./bench -run '^$' -bench BenchmarkCorpusExec -benchmem
WAGO_INLINE=0 go test ./src/core/compiler/backend/railshot -bench=. -benchmem
```

`WAGO_INLINE_MAXBYTES` still controls the encoded-body-size ceiling for inline
candidates.

## Targeted profiles

Backend railshot compile profiles:

```sh
go test ./src/core/compiler/backend/railshot -bench=BenchmarkRailshotCompile -benchmem \
  -memprofile /tmp/railshot_mem.out -cpuprofile /tmp/railshot_cpu.out
go tool pprof -top /tmp/railshot_mem.out
go tool pprof -top /tmp/railshot_cpu.out
```

Frontend decode/validate profiles:

```sh
go test ./src/core/compiler/frontend -bench=. -benchmem \
  -memprofile /tmp/frontend_mem.out -cpuprofile /tmp/frontend_cpu.out
go tool pprof -top /tmp/frontend_mem.out
go tool pprof -top /tmp/frontend_cpu.out
```

Wasm decode/validate profiles:

```sh
go test ./src/core/compiler/wasm -bench=. -benchmem \
  -memprofile /tmp/wasm_mem.out -cpuprofile /tmp/wasm_cpu.out
go tool pprof -top /tmp/wasm_mem.out
go tool pprof -top /tmp/wasm_cpu.out
```

## Interpreting results

- Prefer stable `B/op` and `allocs/op` reductions across repeated runs.
- Treat single-run `ns/op` deltas as noise until `benchstat` or repeated runs
  show a consistent effect.
- Be cautious with microbenchmarks that do not resemble real modules or hot
  production paths.
- Allocation budget tests should be conservative; they should catch obvious
  allocation cliffs without flapping across Go versions or machines.

Benchmarks should keep setup outside the timed loop, call `b.ReportAllocs()`,
and store compiled or decoded results in package-level sinks so the compiler
cannot eliminate the work.

## Do not commit generated profiles

Profile and benchmark output files are disposable local artifacts. Keep them in
`/tmp` or remove them before committing.
