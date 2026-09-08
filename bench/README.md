# wago benchmarks

Use this module to measure performance changes. It is a separate Go module, so
the root package stays dependency-free. Run `go` commands in `bench/`; run
`make` commands from the repository root.

There are two suites:

1. The **comparison suite** (`bench_test.go`) compares `wago` with
   [wazero](https://github.com/tetratelabs/wazero) v1.9 on a fixed program set.
   `./chart` renders its charts.
2. The **stage suite** (`suite_test.go`) measures each wago pipeline stage over
   the curated module corpus in `corpus/`. `./cmd/benchpub` publishes its
   performance history.

Benchmark results depend on the machine and its current load. Use the same
machine and command when you compare two changes.

## Choose a Run

```bash
# In bench/: raw results for all default benchmarks.
go test -bench . -benchmem

# In bench/: one stage, two stages, or the worker-count matrices.
go test -bench '^BenchmarkCompile$' -benchmem
go test -bench 'Decode|Exec' -benchmem
go test -bench '^BenchmarkValidateWorkers' -benchmem
go test -bench '^BenchmarkCompileFullWorkers' -benchmem

# In bench/: include the generated instruction-set micro-suite.
go test -bench . -benchmem -wago.bench.isa

# In bench/: run one benchmark with signal-backed guard-page bounds.
WAGO_BOUNDS=signals go test -tags wago_guardpage \
  -bench '^BenchmarkExec/memory_tree\.run$' -benchmem

# At the repository root: run each corpus once. Guard pages run only where supported.
make bench BENCHTIME=1x BENCH_ISA=1
```

Create local charts and reports from `bench/`:

```bash
go run ./chart                         # wago-versus-wazero charts (gitignored)
go run ./cmd/benchpub -out out         # stage suite to JSON and trend charts
go run ./cmd/benchpub -isa -out out    # include the ISA micro-suite
go run ./cmd/validatestats -runs 30    # repeated validation wall-time stats
../scripts/update-website-bench.mjs    # refresh the sibling website's performance copy
```

## Pipeline Stages and Corpus

`suite_test.go` runs each module in `corpus/` through every applicable stage.
Its result names use `Stage/<module>`.

| Stage | Times |
|---|---|
| `Decode` | Read wasm bytes into a byte-backed `*Module`. Function locals and raw `BodyBytes` remain; no production function-body AST is built. |
| `Validate` | Type-check a decoded module on one worker. |
| `ValidateWorkers` | Type-check function bodies at forced p1, p2, p4, and p8 worker counts. |
| `Compile` | Generate native code for a decoded and validated module. |
| `CompileFull` | Run `wago.Compile`: decode, validate, then compile. |
| `Instantiate` | Set up an instance from a compiled module. |
| `Exec` | Make a host-to-wasm call to each manifest entry point. |

The corpus is listed in `corpus/manifest.json`. Each tier checks in its `.wasm`
files, so normal benchmark runs need no toolchain.

### Synthetic Micro Programs

Each program isolates one code-generation area: micro, loop, calls,
calls+memory, ALU, floating point, memory, globals, control, or scale. The
handwritten `.wat` sources are in `corpus/src/`, with a generator for
`many_funcs`. Regenerate them with `corpus/build.sh`; it needs `wat2wasm`.

The calls+memory fixture, `memory_tree`, is a recursive call tree. It changes
linear memory at each node. It exposes regressions that need both internal calls
and load/store traffic.

### Compute Kernels

These are real algorithms that exercise several areas together. Both subsets run
end to end through the backend:

- Hand-written `.wat` via `corpus/build.sh`: `linked_list` (dependent-load
  pointer chase), `mandelbrot` (f64 escape-time), and `sieve` (memory + strided
  marking + branches).
- **Rust-compiled** via `corpus/build-rust.sh`: the Computer Language Benchmarks
  Game classics `nbody` (leapfrog N-body, f64 mul/add/div/sqrt), `spectralnorm`
  (power iteration, f64 + integer-div inner loop), and `fannkuch` (permutation +
  pancake-flip, pure branch/array churn); `matmul` (dense f64 multiply-add +
  strided memory); `quicksort` (recursive branchy partition + swaps); `crc32`
  and a standards-correct `sha256` (integer hash kernels); and `raytrace`. The
  latter is a recursive Whitted ray tracer with four spheres, a checker plane,
  and depth-4 mirror reflections. It is the corpus's heaviest sustained-f64
  program and its large real program on the compilable execution path. Each
  `corpus/rust/*.rs` source is a self-contained `#![no_std]` cdylib: no imports,
  no heap (fixed stack/static arrays), and one `i32`-count export that returns an
  `i32` DCE sink. Results are deterministic across calls. Regenerate with
  `corpus/build-rust.sh`; it needs `rustc` and
  `rustup target add wasm32-unknown-unknown`. Their golden constants are in
  `corpus_differential_test.go`, which also confirms that explicit and
  guard-page bounds modes agree.

### Third-Party Programs

The `real` tier contains the AssemblyScript libraries `json-as` (JSON
serialize/deserialize), `blake-as` (BLAKE3 hash), and `utf-as` (UTF-8↔UTF-16
transcode). Their host-driven benchmark builds use `assembly/wago-bench.ts` in
each library. That file has an i32-count loop that returns an i32 DCE sink.
Regenerate them with `corpus/build-as.sh`. This needs the AS libraries under
`$AS_ROOT` and `asc`.

Wago has no start section. Therefore, their manifests set `init` to
`_initialize`, which the host calls once after instantiation. A no-op stub
satisfies the `env.abort` import.

The SIMD twins, `json-as-simd`, `blake-as-simd`, and `utf-as-simd`, use
checked-in entry points in `corpus/as/`. Rebuild only them with:

```bash
AS_ROOT=... SIMD_ONLY=1 sh corpus/build-as.sh
```

### Semantic Programs

The checksum-pinned CoreMark, BLAKE3, QOI, LZ4, zlib, and Zstandard artifacts
are in `../tests/corpora`. The benchmark manifest references them in place. This
keeps performance measurements and the exact execution-oracle suite on the same
binaries. They take part in decode, validate, full compile, instantiate, and
paired Wago/wazero execution measurements.

The execution adapter reads authoritative inputs and vectors from
`tests/corpora/MANIFEST.json`. It verifies each exact oracle before timing. Run
`make test-semantic-corpus` for standalone result checks. Both suites enable LZ4
compression and decompression.

### Large Real-World Programs

The `real-large` tier contains whole programs: the `wasm3` interpreter, the
`lua` (Lua 5.4) interpreter, the `sqlite3` (SQLite 3.46) engine, the
multi-megabyte `ruby` (Ruby 3.3, ~16 MiB, ~17k functions), and `esbuild` (a
Go-to-wasm bundler, ~12 MiB). Wago benchmarks their full compile path.

Their command execution is an optional compatibility integration, not a
core-runtime benchmark. From the Emscripten plugin checkout, run:

```bash
WAGO_CORPUS_DIR=/path/to/wago/bench/corpus go test ./...
```

This exercises regexmatch, wasm3, Lua, SQLite, Ruby, and esbuild with checked
workload results.

### ISA Micro-Suite

Enable this opt-in suite with `-wago.bench.isa`, `benchpub -isa`, or
`make bench BENCH_ISA=1`. It has one exported function for each individual
opcode: i32/i64 arithmetic, logic, shifts, division, and bit count; f32/f64
arithmetic, min/max, square root, and rounding; sequential and strided memory
loads/stores; bulk-memory copy and fill at 64 B, 256 B, and 4 KiB; control
operations; direct and indirect calls; local/global access; and width/type
conversions.

Each function uses a coupled dual-accumulator dependent chain
(`a=a OP b; b=b OP a`). This prevents instruction-level parallelism (ILP),
common-subexpression elimination (CSE), constant folding, and dead-code
elimination (DCE) from hiding latency. The raw ns/op is therefore comparable
between opcodes and engines. Use this tier to find primitive code-generation
gaps.

`corpus/gen` generates the `.wat` files and standalone `corpus/isa-manifest.json`.
Run `corpus/build.sh` to regenerate them. Compare an opcode family across engines
with `go test -run '^$' -bench 'Exec/isa_f64' -count 6 .` for wago, then compare
the matching `WazeroExec/isa_f64` rows. Each bulk-memory function repeats its
operation 256 times per host call. This amortizes engine call overhead in the
same way for each engine.

The shared AMD64/ARM64 SIMD set includes vector bitwise operations; integer
arithmetic, shifts, comparisons, saturation, narrowing, widening, extmul, dot
products, and reductions at each lane width; plus packed f32/f64 arithmetic,
comparisons, rounding, min/max, and integer conversions. Generate a median
comparison table from repeated runs with:

```bash
go test -run '^$' -bench '^(BenchmarkExec|BenchmarkWazeroExec)/isa_' \
  -wago.bench.isa -benchmem -benchtime=100ms -count=3 > isa.txt
go run ./cmd/isatable -input isa.txt -out isa.md -cpu "$(uname -m)"
```

A module may not support every stage. The manifest can limit its stages, and the
backend can reject compilation. The suite does not benchmark such stages.
Optional binaries can use manifest `path` entries. They are skipped when absent;
see `corpus/fetch.sh`.

## Cross-engine comparison

`compare_test.go` runs wazero's `CompileModule` and execution paths over the
same corpus. `benchpub -warp <harness>` can also call WARP's native harness for
compile and execution. Build `vb_bench` from an independent
[WARP checkout](https://github.com/wago-org/warp), use the published-comparison
configuration, and pass the harness path explicitly.

Instantiation is paired too. `BenchmarkInstantiate` reuses Wago's compiled
module. `BenchmarkWazeroInstantiate` reuses wazero's compiled module. Each then
measures a new instance with equivalent supported imports. The suite creates two
additional charts:

- `compile-engines.svg` — compile time per module, wago vs wazero vs WARP. Where
  the backend can't compile a module yet, wago's **validate** time is shown
  (dimmed) so the big binaries still appear.
- `exec-engines.svg` — execution time per export, wago vs wazero vs WARP, on the
  real workloads (same manifest args for all three engines).

wazero compiles every corpus module. The comparison therefore shows where
wago's single-pass compiler wins on small modules and where byte-backed decode
and validation still dominate on very large inputs.

## Performance History

For validator work, `cmd/validatestats` repeats the normal validation path and
reports its average, median, and maximum wall-clock duration:

```bash
# In bench/.
go run ./cmd/validatestats -runs 30 -warmup 5  # full corpus
go run ./cmd/validatestats -file ../tests/fixtures/wasm/fib.wasm
```

This is the serial `wago validate <file>` path: byte-backed `DecodeModule` then
`ValidateModule`. Use `BenchmarkValidateWorkers` to measure parallel validation.
Neither path builds or verifies IR.

`cmd/benchpub` runs the stage suite and writes a versioned JSON record. The
record has `git describe`, commit, date, and CPU information. It appends to
`history.json` and renders per-stage latency for this run and trends across
versions.

When the manifest enables real-world modules (`compute`, `real`, or
`real-large`), it also creates `realworld.svg`. It groups modules by wasm size
and shows one bar for each pipeline stage reached. Large modules that only
decode or validate appear as gaps. The default synthetic-only manifest does not
create this chart.

`scripts/publish-bench.sh` publishes to
[`wago-org/docs`](https://github.com/wago-org/docs), where the history is kept
in that repository's `docs/bench/` directory:

```bash
./scripts/publish-bench.sh  # run, append history, and push charts (stable machine)
```

`make bench-website` updates the sibling `../website` checkout from the latest
`bench/.bench-run.txt`. It syncs website statistics and rebuilds `dist/`.
`make bench-publish` performs the same update automatically when `../website`
exists.

With architecture tabs, one `bench/out/bench.json` updates only its matching
`goarch` panel and keeps measurements from other machines. Set
`WAGO_BENCH_JSON_AMD64` and `WAGO_BENCH_JSON_ARM64` to rebuild both panels from
JSON snapshots. The website omits rows that lack either Wago or wazero results.

## Benchmarks in the Comparison Suite

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

`go run ./chart` renders zero-dependency SVG bar charts in `bench/charts/`.
That directory is ignored by Git. The renderer is pure Go; it does not need
Chart.js or a browser.

- `speedup.svg` — speedup vs wazero per benchmark (log scale; green = wago
  faster, red = slower)
- `latency.svg` — ns/op, wago vs wazero (grouped, log scale)

Published copies live in [`wago-org/docs`](https://github.com/wago-org/docs)
under `charts/`. The root README embeds them by raw URL. From the repository
root, run `./scripts/publish-charts.sh` on a stable machine to regenerate and
push them. CI never renders benchmark charts because shared runners add noise.
