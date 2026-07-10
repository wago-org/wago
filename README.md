<h1 align="center"><pre>╦ ╦ ╔═╗ ╔═╗ ╔═╗
║║║ ╠═╣ ║ ╦ ║ ║
╚╩╝ ╩ ╩ ╚═╝ ╚═╝</pre></h1>

<p align="center">
  A pure-Go, no-cgo WebAssembly JIT for low-latency host ↔ wasm execution.
</p>

<details>
<summary>Table of Contents</summary>

- [Installation](#installation)
- [Docs](#docs)
- [Usage](#usage)
  - [Run a module](#run-a-module)
  - [Run a WASI command](#run-a-wasi-command)
  - [Inspect plugins and imports](#inspect-plugins-and-imports)
- [Go API](#go-api)
  - [Compile and invoke](#compile-and-invoke)
  - [Typed runtime calls](#typed-runtime-calls)
  - [Host imports](#host-imports)
  - [Memory](#memory)
  - [Globals, tables, and cross-instance linking](#globals-tables-and-cross-instance-linking)
  - [Plugins and policies](#plugins-and-policies)
  - [Precompiled modules](#precompiled-modules)
- [Feature Support](#feature-support)
  - [WebAssembly Core](#webassembly-core)
  - [Runtime and product surface](#runtime-and-product-surface)
  - [Built-in plugins](#built-in-plugins)
  - [Current limits](#current-limits)
- [Performance](#performance)
  - [Runtime comparison](#runtime-comparison)
  - [Startup latency](#startup-latency)
  - [Binary size](#binary-size)
  - [Performance tuning](#performance-tuning)
  - [Running benchmarks locally](#running-benchmarks-locally)
- [Configuration](#configuration)
- [Debugging](#debugging)
- [Architecture](#architecture)
- [Project layout](#project-layout)
- [Development](#development)
- [Contributing](#contributing)
- [License](#license)
- [Contact](#contact)

</details>

## Installation

During private development, the installer builds from source over SSH. You need
read access to `git@github.com:wago-org/wago` and Go 1.22+:

```bash
curl -fsSL https://wago.sh/install.sh | sh
```

The same command is intended to install a public prebuilt binary after the
`v0.1.0` release. Until then, useful installer knobs are:

| Variable | Meaning |
|---|---|
| `WAGO_VERSION` | Git ref to build: branch, tag, or commit. Defaults to `main`. |
| `WAGO_BIN_DIR` | Install directory. Defaults to `~/.local/bin`. |
| `WAGO_DRY_RUN=1` | Print the source-build plan without installing. |
| `NO_COLOR=1` | Disable colored installer output. |

From a checkout:

```bash
go build -o wago ./cli/wago
go install ./cli/wago
```

For library use:

```bash
go get github.com/wago-org/wago
```

Current target: **linux/amd64**. The standard build uses Go's toolchain; the
lean release build uses TinyGo and still stays no-cgo.

## Docs

The high-level project docs live in this repo:

- [FEATURES.md](FEATURES.md) — WebAssembly support matrix.
- [ROADMAP.md](ROADMAP.md) — near-term engine and product roadmap.
- [ARCHITECTURE.md](ARCHITECTURE.md) — pipeline, runtime, ABI, and design notes.
- [OPTIMIZATIONS.md](OPTIMIZATIONS.md) — current and planned codegen work.
- [plugins/wasi/README.md](plugins/wasi/README.md) — WASI plugin usage and coverage.
- [examples/README.md](examples/README.md) — runnable Go API examples.
- [bench/README.md](bench/README.md) — benchmark corpus and publishing flow.

## Usage

### Run a module

`wago run` compiles a raw `.wasm` module and invokes an export. `run` is also the
default command, so `wago file.wasm ...` works too.

```bash
wago run tests/testdata/fib.wasm 30
wago run -e hypot tests/testdata/fprog.wasm 3.0 4.0
wago tests/testdata/fib.wasm 30
```

Arguments are typed from the export signature. Override one argument with a
suffix when the default parser is not enough:

```bash
wago run -e hypot tests/testdata/fprog.wasm 3:f64 4:f64
```

Validate without executing:

```bash
wago validate tests/testdata/fib.wasm
```

`wago build` is reserved for the future `.wago` product path and currently
returns `not implemented`.

### Run a WASI command

A module exporting `_start` runs as a command. Add the WASI plugin for argv,
env, stdio, clocks, random, and `proc_exit` handling:

```bash
wago run --plugin wasi program.wasm arg1 arg2
wago run --plugin wasi/p1 program.wasm
wago run --plugin wasi/unstable old-program.wasm
```

`wasi` is preview 1 by default. `wasi/p2` is reserved for the component-model
preview 2 surface and errors until implemented.

### Inspect plugins and imports

The CLI can show which plugins are compiled into the binary and what imports a
module needs:

```bash
wago plugin list
wago plugin inspect wasi
wago plugin inspect wasi --json

wago module imports app.wasm
wago module capabilities app.wasm
wago env
wago version list
```

Built-in plugins in the standard CLI are `timer`, `log`, `metrics`, `wasi`,
`wasi/p1`, and `wasi/unstable`.

## Go API

### Compile and invoke

The low-level API uses raw 8-byte wasm call slots. Encode arguments with
`wago.I32`, `I64`, `F32`, `F64`; decode results with `AsI32`, `AsI64`, `AsF32`,
and `AsF64`.

```go
package main

import (
	"fmt"
	"os"

	"github.com/wago-org/wago"
)

func main() {
	src, err := os.ReadFile("tests/testdata/fprog.wasm")
	if err != nil {
		panic(err)
	}

	mod, err := wago.Compile(src)
	if err != nil {
		panic(err)
	}
	inst, err := wago.Instantiate(mod, nil)
	if err != nil {
		panic(err)
	}
	defer inst.Close()

	out, err := inst.Invoke("hypot", wago.F64(3), wago.F64(4))
	if err != nil {
		panic(err)
	}
	fmt.Println(wago.AsF64(out[0])) // 5
}
```

Compile once, instantiate many times when the same module is used repeatedly.

### Typed runtime calls

`Runtime` is the higher-level entry point. It carries config, plugins, hooks, and
policy metadata, and exposes typed `Value` calls with `context.Context`.

```go
rt := wago.NewRuntime()
defer rt.Close()

mod, err := rt.Compile(wasmBytes)
if err != nil {
	panic(err)
}

inst, err := rt.Instantiate(context.Background(), mod)
if err != nil {
	panic(err)
}
defer inst.Close()

out, err := inst.Call(context.Background(), "add", wago.ValueI32(2), wago.ValueI32(40))
if err != nil {
	panic(err)
}
fmt.Println(out[0].I32())
```

Use `mod.Exports()`, `mod.Imports()`, `mod.RequiredCapabilities()`, and
`mod.Metadata()` for lightweight inspection.

### Host imports

Host functions use one reflection-free stack form. This is the same form used by
plugins and it works under both Go and TinyGo:

```go
mul := wago.HostFunc(func(_ wago.HostModule, params, results []uint64) {
	a := wago.AsI32(params[0])
	b := wago.AsI32(params[1])
	results[0] = wago.I32(a * b)
})

inst, err := wago.Instantiate(compiled, wago.Imports{
	"host.mul": mul,
})
```

`HostModule` gives the host function access to the calling instance's memory:

```go
logString := wago.HostFunc(func(m wago.HostModule, params, results []uint64) {
	ptr := uint32(wago.AsI32(params[0]))
	n := uint32(wago.AsI32(params[1]))
	mem := m.Memory()
	if uint64(ptr)+uint64(n) > uint64(len(mem)) {
		results[0] = wago.I32(-1)
		return
	}
	fmt.Println(string(mem[ptr : ptr+n]))
	results[0] = wago.I32(0)
})
```

Host imports can take and return numeric scalars and `v128`. The public `v128`
representation is `wago.V128`, a `[16]byte`.

### Memory

`Instance.Memory().Bytes()` returns the same mmap-backed linear memory the native
wasm code sees. There is no copy between host and guest.

For hot host-side reads and writes, use the typed accessors. They are
bounds-checked and avoid the slower `encoding/binary` pattern under TinyGo:

```go
v, ok := inst.ReadUint32Le(off)
if ok {
	inst.WriteFloat64Le(off+8, float64(v))
}

buf, ok := inst.Read(ptr, length)
_ = inst.Write(ptr, []byte("hello"))
```

Out-of-bounds reads return `ok=false`; out-of-bounds writes return `false` and do
not modify memory.

### Globals, tables, and cross-instance linking

Wago supports numeric and `v128` globals, module-local `funcref` globals,
mutable numeric global imports/exports, exact named indexed funcref table exports,
multiple imported/shared funcref tables followed by local tables, memory
imports/exports, and cross-instance function calls. Externref signatures,
locals/control flow, public generation-checked handles, and reflection-free host
round trips are executable. Imported reference globals, broader host funcref
boundaries, and externref global/table storage remain WebAssembly 2.0 closeout
work.

```go
counter := wago.NewGlobalI32(10, true)
defer counter.Close()

mem, err := wago.NewMemory(1, 8)
if err != nil {
	panic(err)
}
defer mem.Close()

inst, err := wago.Instantiate(compiled, wago.Imports{
	"env.counter": wago.GlobalImport{Global: counter},
	"env.memory":  mem,
})
```

The shared `*Global`, `*Memory`, and `*Table` objects are the host-owned cells.
Multiple instances importing the same object observe the same state.

### Plugins and policies

An extension declares its identity, capabilities, host imports, and hooks through
`Registry`.

```go
type randExt struct{}

func (randExt) Info() wago.ExtensionInfo {
	return wago.ExtensionInfo{
		ID:          "example.rand",
		Name:        "Rand",
		Version:     "1.0.0",
		Description: "Pseudo-random numbers for guests.",
		Stability:   wago.Experimental,
		Compat: wago.Compatibility{
			Engines: map[string]string{"wago": ">=0.1.0"},
		},
	}
}

func (randExt) Register(reg *wago.Registry) error {
	const capRand = wago.Capability("rand.read")
	reg.Capability(capRand, wago.CapabilityDocs("read pseudo-random numbers"))
	reg.ImportModule("wago_rand").
		Func("next", func(_ wago.HostModule, _ []uint64, results []uint64) {
			results[0] = wago.I64(4)
		}).
		Results(wago.ValI64).
		Capability(capRand)
	return nil
}

rt := wago.NewRuntime()
_ = rt.Use(randExt{})
```

Policies can allow or deny capabilities and enforce coarse declared resource
limits at instantiation:

```go
inst, err := rt.Instantiate(ctx, mod, wago.WithPolicy(wago.Policy{
	AllowedCapabilities: []wago.Capability{wago.CapTimerRead},
	MaxMemoryBytes:      16 << 20,
	MaxTableEntries:     1024,
}))
```

### Precompiled modules

The Go API can serialize compiled modules to `.wago` blobs and load them later:

```go
compiled, err := wago.Compile(wasmBytes)
blob, err := compiled.MarshalBinary()

compiled, err = wago.Load(blob)      // precompiled .wago
compiled, err = wago.Load(wasmBytes) // raw wasm, compiled on load
```

The CLI can run an existing `.wago` blob, but producing stable, cache-keyed
`.wago` artifacts from the CLI is still on the roadmap.

## Feature Support

Status: done means decoded, validated, compiled, and covered by tests or
conformance where applicable. Partial means the feature family is admitted only
for the listed subset. [FEATURES.md](FEATURES.md) is the source of truth.

### WebAssembly Core

| Feature | Status |
|---|---|
| WebAssembly 1.0 MVP scalar semantics | Done. The pinned MVP spec suite reports 57/57 applicable files passing, 16,592 passing assertions, 0 failing assertions. |
| Numeric types | `i32`, `i64`, `f32`, `f64`, and `v128`. |
| Integer ops | Arithmetic, bitwise, shifts/rotates, div/rem traps, clz/ctz/popcnt, comparisons. |
| Float ops | Add/sub/mul/div/sqrt/abs/neg/min/max, comparisons, rounding ops, conversions, reinterprets, NaN/overflow trunc traps. |
| Control flow | `block`, `loop`, `if`, `else`, `br`, `br_if`, `br_table`, `return`, `select`, `select t`. |
| Calls | Direct calls, recursion, `call_indirect` with table bounds and signature checks. |
| Linear memory | All MVP load/store widths, `memory.size`, `memory.grow`, active data segments. |
| Globals | Numeric and `v128` globals with mutable imports/exports, plus module-local nullable/mutable `funcref` globals and typed host access. Imported reference globals remain pending. |
| Tables | Funcref tables, passive/active elements, every `table.*` operation, multiple local and imported tables, nonzero-table `call_indirect`, exact indexed exports/re-exports, duplicate imported aliases, and host functions as table funcrefs. Externref tables remain pending. |
| Imports/exports | Functions, numeric/vector globals, memories, and indexed funcref tables including multiple shared imports followed by local definitions with exact names; cross-instance linking uses link-time recompile and context swap. |
| Start function | Local start functions and imported void host start functions. |
| Sign extension | Done: all five scalar `i32`/`i64.extend{8,16,32}_s` opcodes are decoded, validated, lowered, and covered by runtime/codegen tests. |
| Non-trapping float-to-int | `trunc_sat` done. |
| Bulk memory | Done for linear memory and funcref tables: copy/fill/init/drop operations plus passive data and element segments execute. Externref table storage remains part of reference-types completion. |
| Multi-value | Done semantically for functions, blocks, branches, calls, public invocation, and compiled metadata; a wider optimized result ABI remains a performance task. |
| Reference types | Partial: nullable/local `funcref`, structural `ref.func`, typed `select`, local funcref globals, multiple local/imported tables, indexed table operations/calls, duplicate import aliases, and exact named table exports/re-exports execute. Externref signatures, locals/control flow, public generation-checked handles, and reflection-free host params/results also execute. Externref globals/tables plus remaining host/shared funcref/global boundaries are pending. |
| SIMD | Done for the documented linux/amd64 baseline: SSSE3/SSE4.1 plus AVX/VEX.128. Core SIMD and deterministic relaxed SIMD opcodes through `0xfd 275` are decoded, validated, and lowered. |
| Threads and atomics | Planned. |
| Tail calls | Planned. |
| Multi-memory | Not planned. |
| Exceptions and wasm GC proposals | Not planned for now. |

### Runtime and product surface

| Area | Status |
|---|---|
| No-cgo execution | Done: W^X mmap, foreign-stack trampoline, trap-to-error path, zero-copy linear memory. |
| Bounds checks | Explicit checks by default; signals/guard-page mode behind `-tags wago_guardpage` and `WAGO_BOUNDS=signals`. |
| Runtime config | Done: immutable wazero-style `RuntimeConfig`, feature gating, memory page limit, bounds mode, deferred bounds-check facts. |
| Synchronous host calls | Done: host imports can return results, including `v128`. |
| Plugins | Done: extension metadata, capability declarations, host imports, hooks, CLI inspection, manifest commands. |
| Policy | Partial: capability allow/deny plus memory/table limits are enforced; invoke duration and process/mailbox resource limits are reserved. |
| Instance pools | Done: `Class`, `Acquire`/`Release`, warm pool, reset policies. |
| Process layer | Experimental: `Spawn`, `Send`, `Monitor`, `Link`, `Kill`, mailboxes, and supervisors. |
| `.wago` blobs | Go API serialization/loading works; CLI build/cache productization is planned. |
| Version management | Local list/use/current/which/uninstall path is present; network install is build-dependent. |
| TinyGo | Supported on linux/amd64 with `-scheduler=tasks`; release builds are size-focused. |

### Built-in plugins

| Plugin | Capability | Imports |
|---|---|---|
| `timer` | `timer.read` | Wall-clock ms, monotonic ns, sleep ms. |
| `log` | none | Structured guest logging through `wago_log.write`. |
| `metrics` | `metrics.write` | Counters and histograms. |
| `wasi`, `wasi/p1` | `wasi` | Minimal WASI preview 1: stdio, args/env, clocks, random, `proc_exit`, selected fd calls. |
| `wasi/unstable` | `wasi` | Pre-preview1 `wasi_unstable` module name over the same core. |
| `wasi/p2` | none | Placeholder; not implemented. |

### Current limits

- Platform support is linux/amd64.
- The CPU baseline for SIMD is SSSE3/SSE4.1 plus AVX/VEX.128. AVX2/FMA/VNNI are
  future feature-gated fast paths, not baseline requirements.
- WASI is intentionally minimal. Filesystem, sockets, and polling imports are
  stubbed with clean errno values unless implemented.
- Wago is JIT-only. There is no interpreter tier.
- Unsupported or disabled wasm features are rejected at compile time rather than
  accepted and mis-run.

## Performance

Wago is tuned for fast cold compilation, low host-call overhead, and small
operational footprint. It is not an optimizing tier; the backend is a direct
single-pass compiler based on WARP's Valent-Block design.

### Runtime comparison

The local benchmark suite compares Wago with wazero and WARP over synthetic
micro modules, compute kernels, AssemblyScript libraries, and large real-world
modules.

Latest checked-in benchmark dump:
[bench/out/bench.json](bench/out/bench.json), `nightly-96-g6e73d12`
(`2026-07-05T00:25:47-07:00`), AMD Ryzen 7 7800X3D, linux/amd64.

| Benchmark | wago | wazero | Delta |
|---|---:|---:|---:|
| Compile `tiny` | 3.2 us | 62 us | 20x faster |
| Compile `fib_rec` | 4.7 us | 65 us | 14x faster |
| Compile `memory_tree` | 13 us | 112 us | 8.6x faster |
| Compile `json-as` | 1.08 ms | 5.85 ms | 5.4x faster |
| Exec `tiny.add` | 16.8 ns | 28.3 ns | 1.7x faster |
| Exec `fib_iter.fib` | 28.1 ns | 34.4 ns | 1.2x faster |
| Exec `fib_rec.fib` | 1.48 ms | 2.18 ms | 1.5x faster |
| Exec `memory_tree.run` | 9.6 us | 22 us | 2.3x faster |
| Exec `json-as.serializeN` | 21 us | 32 us | 1.5x faster |
| Exec `json-as.deserializeN` | 39 us | 64 us | 1.6x faster |
| SQLite query | 514 us | 984 us | 1.9x faster |

Focused json-as SWAR microbenchmarks from the same dump:

| Benchmark | wago | wazero | Delta |
|---|---:|---:|---:|
| `JSON.stringify` path | 119.8 ns | 159.3 ns | 1.3x faster |
| `JSON.parse` path | 219.6 ns | 317.7 ns | 1.4x faster |

Conformance and project stats synced into the sibling website dump
(`../website/data/stats.json` locally) on `2026-07-06`: 57/57 MVP files pass,
16,592 assertions pass, 0 fail, 0 lines of cgo, and 79% generated test coverage.

### Startup latency

The startup study in [docs/startup-latency-2026-07.md](docs/startup-latency-2026-07.md)
measures full process startup on a real `json-as` wasm workload:

| Runtime | Type | Total | Startup noop | Approx exec |
|---|---|---:|---:|---:|
| wasm3 0.5.2 | interpreter | 5.0 ms | 1.2 ms | 3.8 ms |
| wago dev @ `0df7ea2` | single-pass JIT | 5.4 ms | 5.0 ms | 0.4 ms |
| wasmtime 45.0.1 | Cranelift JIT | 8.0 ms | 7.3 ms | 0.7 ms |
| wazero 1.12.0 | compiler | 10.8 ms | 8.8 ms | 2.0 ms |
| wasmer 7.1.0 cranelift | optimizing JIT | 21.3 ms | 21.1 ms | ~0.2 ms |
| wavm LLVM 21.1.8 | LLVM JIT | 263 ms | 265 ms | ~0 |

That snapshot was taken on an AMD Ryzen 7 7800X3D linux/amd64 machine with
compilation caches disabled for cold rows. Treat it as a point-in-time engineering
measurement, not a universal claim.

### Binary size

From [docs/tinygo.md](docs/tinygo.md), linux/amd64 CLI size snapshots:

| Build | Size |
|---|---:|
| `go build` default | 3.1 MB |
| `go build -ldflags="-s -w"` | 2.1 MB |
| `tinygo build -no-debug -opt=z -gc=conservative` + `strip -s` | 0.43 MB |
| Above plus UPX | 0.16 MB |

`make build-release` uses the TinyGo size path. Build with `-scheduler=tasks` when
using TinyGo; see the TinyGo doc for the foreign-stack and GC rationale.

### Performance tuning

| Knob | Meaning |
|---|---|
| `WAGO_BOUNDS=signals` | Use guard-page bounds checks when the binary was built with `-tags wago_guardpage`. |
| `WAGO_BOUNDS=explicit` | Force inline explicit bounds checks. |
| `--bounds defer` | CLI default: skip provably redundant explicit checks in straight-line regions. |
| `--bounds all` | CLI A/B mode: check every explicit memory access. |
| `WAGO_NO_BOUNDS_FACTS=1` | Disable deferred bounds-check facts globally. |
| `RuntimeConfig.WithFeature` | Accept or reject individual wasm feature families. |
| `RuntimeConfig.WithMemoryLimitPages` | Cap declared linear memory in 64 KiB wasm pages. |

Guard-page mode is faster on memory-heavy modules but installs process-wide signal
handlers and must be selected deliberately in builds that include it.

### Running benchmarks locally

The benchmark suite is a separate Go module under [bench/](bench/).

```bash
cd bench
go test -bench . -benchmem
go test -bench '^BenchmarkCompile$' -benchmem
go test -bench 'Decode|Exec' -benchmem
```

Include the generated ISA micro-suite only when you want opcode-level coverage:

```bash
go test -bench . -benchmem -wago.bench.isa
go run ./cmd/benchpub -isa -out out
```

Run the guard-page path:

```bash
WAGO_BOUNDS=signals go test -tags wago_guardpage \
  -bench '^BenchmarkExec/memory_tree\.run$' -benchmem
```

Generate charts and benchmark history:

```bash
go run ./chart
go run ./cmd/benchpub -out out
```

Compare against WARP:

```bash
make bench-warp
make bench WARP=auto
```

The corpus includes hand-written `.wat` micro modules, Rust kernels,
AssemblyScript libraries such as `json-as`, `blake-as`, and `utf-as`, WASI
programs, and large decode/validate inputs such as Lua, SQLite, Ruby, and
esbuild. See [bench/README.md](bench/README.md) for the full map.

## Configuration

Wago's runtime config is immutable. Every `WithXxx` method returns a copy:

```go
cfg := wago.NewRuntimeConfig().
	WithFeature(wago.CoreFeatureBulkMemoryOperations, false).
	WithMemoryLimitPages(256)

if wago.GuardPageSupported() {
	cfg = cfg.WithBoundsChecks(wago.BoundsChecksSignalsBased)
}

compiled, err := cfg.Compile(wasmBytes)
```

The default feature set is what the current backend can lower: mutable globals,
sign-extension ops, supported bulk-memory subset, non-trapping float-to-int, and
SIMD when the host CPU supports the documented baseline.

`CoreFeaturesV2` is the static WebAssembly 2.0 release group, including core
SIMD. It is not a runtime capability probe or a claim that every partially
implemented family is complete. Use `SupportedFeatures()` for build- and
host-admitted feature gates; on CPUs below the documented SIMD baseline it
clears `CoreFeatureSIMD`.

Use `SupportedFeatures()` for portable program setup:

```go
features := wago.SupportedFeatures()
if !features.IsEnabled(wago.CoreFeatureSIMD) {
	// choose a non-SIMD module or reject early
}
```

## Debugging

Useful commands:

```bash
wago --version
wago env
wago module imports app.wasm
wago module capabilities app.wasm
wago plugin inspect wasi --json
```

Developer and benchmark diagnostics:

| Tool | Use |
|---|---|
| `WAGO_EXPLAIN=1` | Emit compile/codegen explanation when built on the codegen-stats path. |
| `bench/cmd/explain` | Inspect codegen counters and disassembly-oriented output. |
| `make spec1` / `make spec2` | Run the separately pinned WebAssembly 1.0 baseline or official 2.0 core corpus through `wast2json` (see `docs/spec-testing.md`). |
| `WAGO_WASITEST_DIR=/path/to/wasi-testsuite make wasi-suite` | Run the WASI preview 1 testsuite. |
| `make test-guard` | Run guard-page focused tests. |

## Architecture

The execution pipeline is intentionally direct:

```text
wasm bytes
  -> src/core/compiler/wasm
       strict binary decode, custom-section parsing, validation, feature gating
  -> src/core/compiler/backend/railshot
       single-pass x86-64 codegen over validated byte-backed bodies
  -> src/core/runtime
       W^X mmap, foreign-stack trampoline, traps, linear memory, linking
```

Important design choices:

- **No cgo.** Native wasm code is entered through a Go/TinyGo-compatible
  trampoline, not a C boundary.
- **JIT-only.** There is no interpreter fallback. Unsupported features are
  rejected.
- **Strict decoding.** Malformed structured custom sections, including malformed
  `name` sections, are decode errors.
- **Byte-backed frontend.** Production compile does not materialize full
  instruction ASTs for function bodies.
- **Single-pass backend.** Railshot uses a symbolic operand stack, pinned locals
  and globals, deterministic join slots, and shared cold trap stubs.
- **Zero-copy memory.** Host and guest see the same mmap-backed linear memory.
- **Auditable boundaries.** Unsafe, mmap, stack switching, traps, and host calls
  are kept in narrow runtime files and covered by focused tests.

For the full internal tour, see [ARCHITECTURE.md](ARCHITECTURE.md) and
[docs/runtime-abi.md](docs/runtime-abi.md).

## Project layout

```text
.
  wago.go                              public facade over src/wago
  cli/wago/                            CLI: run, validate, plugins, modules, versions
  src/wago/                            public runtime API implementation
  src/core/compiler/wasm/              decoder, validator, feature support
  src/core/compiler/backend/railshot/  single-pass linux/amd64 JIT backend
  src/core/runtime/                    no-cgo execution runtime
  plugins/                             built-in extensions: timer, log, metrics, WASI
  examples/                            runnable API examples
  tests/testdata/                      small wasm fixtures
  tests/spec/                          WebAssembly spec submodule
  tests/wasi/                          WASI testsuite submodule
  bench/                               benchmark corpus, charts, cross-engine comparisons
  docs/                                design notes, performance plans, workflow docs
  warp/                                reference C++ WARP tree
```

## Development

Common checks:

```bash
make lint
make test
make test-guard
cd bench && go test ./...
```

Spec and WASI conformance:

```bash
make spec        # needs wabt's wast2json and tests/spec
make wasi-suite  # needs tests/wasi
```

Builds:

```bash
make build
make build-release
make tinygo-build
make tinygo-test
```

Coverage:

```bash
make cover
```

Current generated coverage summary in [coverage-report.md](coverage-report.md):
79.2% overall, with the wasm test helpers at 100% and the public `wago` package
above 83%.

## Contributing

Please see [CONTRIBUTING.md](CONTRIBUTING.md). The short version:

- keep changes narrow and auditable;
- add or update tests with behavior changes;
- run the most relevant tests and say what you did not run;
- include numbers for compiler, runtime, host-call, memory, or footprint claims;
- update docs in `docs/` when workflow, testing, benchmarking, review
  expectations, or agent behavior changes.

## License

Wago is licensed under [Apache-2.0](LICENSE).

The reference [warp/](warp/) tree keeps its original license headers.

## Contact

Open an issue or discussion in the project repository. For project updates and
installer entry point, see <https://wago.sh/>.
