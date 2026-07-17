# Codegen benchmarks and profiling

Focused compiler-path benchmarks cover byte-backed frontend decode/validate,
railshot backend compilation, and decode/validate/compile end-to-end paths.
Generated benchmark logs and profiles such as `cpu.out`, `mem.out`,
`/tmp/railshot-before.txt`, and `/tmp/frontend_cpu.out` are local artifacts;
do not commit them. Prefer `/tmp/...` paths in examples so profiling does not
clutter the repository root.

## Stable benchmark runs

The corpus suite is a separate Go module. Use repeated stage-specific runs when
comparing performance:

```sh
cd bench
go test . -run '^$' -bench 'BenchmarkCompile/(sqlite3|ruby|esbuild)$' \
  -benchmem -benchtime=1x -count=5
go test . -run '^$' -bench 'BenchmarkValidate/(sqlite3|ruby|esbuild)$' \
  -benchmem -benchtime=1x -count=5
```

Optional `benchstat` workflow:

```sh
cd bench
go test . -run '^$' -bench 'BenchmarkCompile/(sqlite3|ruby|esbuild)$' \
  -benchmem -benchtime=1x -count=10 > /tmp/railshot-before.txt
# make changes
go test . -run '^$' -bench 'BenchmarkCompile/(sqlite3|ruby|esbuild)$' \
  -benchmem -benchtime=1x -count=10 > /tmp/railshot-after.txt
benchstat /tmp/railshot-before.txt /tmp/railshot-after.txt
```

## Compile-memory reference (2026-07-17)

Apple M4 Max, darwin/arm64, explicit bounds. `main` and the compile-memory
refactor were measured from prebuilt benchmark binaries with `-benchtime=1x`.
`B/op` is cumulative allocation; RSS includes the benchmark process and corpus
loader, so it is a deliberately conservative whole-process measurement.

| stage/corpus | main B/op | refactor B/op | reduction |
|---|---:|---:|---:|
| Railshot / sqlite3 | 45,528,608 | 9,177,056 | 79.8% |
| Railshot / ruby | 558,089,848 | 88,589,296 | 84.1% |
| Railshot / esbuild | 522,828,072 | 73,553,168 | 85.9% |
| full compile / sqlite3 | 50,363,176 | 16,049,616 | 68.1% |
| full compile / ruby | 587,471,976 | 114,503,176 | 80.5% |
| full compile / esbuild | 572,585,912 | 101,079,816 | 82.3% |

Ruby Railshot peak RSS fell from 209,780,736 to 130,400,256 bytes (37.8%);
esbuild fell from 190,447,616 to 123,043,840 bytes (35.4%). Adjacent compile
samples were faster rather than slower; longer execution checks remained
zero-allocation and within normal run-to-run variation of main.

For compile-memory work, report both cumulative allocation (`B/op`) and peak
process RSS. They measure different things: reusable scratch can sharply reduce
allocation traffic while retained corpus bytes and native output still set a
larger live-memory floor. Build the benchmark binary once before an RSS run so
the Go build is outside the measurement:

```sh
cd bench
go test -c -o /tmp/wago-bench.test .
/usr/bin/time -l /tmp/wago-bench.test -test.run '^$' \
  -test.bench '^BenchmarkCompile$/^ruby$' -test.benchtime=1x -test.benchmem
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
cd bench
go test . -run '^$' -bench '^BenchmarkCompile$/^ruby$' -benchtime=1x \
  -benchmem -memprofile /tmp/railshot_mem.out -cpuprofile /tmp/railshot_cpu.out
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
