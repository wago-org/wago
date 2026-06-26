<h1 align="center"><pre>╦ ╦ ╔═╗ ╔═╗ ╔═╗
║║║ ╠═╣ ║ ╦ ║ ║
╚╩╝ ╩ ╩ ╚═╝ ╚═╝</pre></h1>

<p align="center">
  A pure-Go WebAssembly JIT built for low-latency host ↔ wasm calls.
</p>

<details>
<summary>Table of Contents</summary>

- [What](#what)
- [Installation](#installation)
- [Usage](#usage)
  - [CLI](#cli)
  - [Go API](#go-api)
- [API](#api)
  - [`Value`](#value)
  - [`Compile` / `Load`](#compile--load)
  - [`Instance`](#instance)
  - [Host imports](#host-imports)
- [Feature Support](#feature-support)
- [Performance](#performance)
- [Architecture](#architecture)
- [Project Layout](#project-layout)
- [Running Tests](#running-tests)
- [Contributing](#contributing)
- [License](#license)
- [Contact](#contact)

</details>

## What

`wago` is a **no-cgo** WebAssembly engine for Go. It decodes, validates,
compiles, and runs wasm modules through a single-pass x86-64 backend.

It borrows the host-boundary shape from [WARP](warp/), BMW's C++ single-pass
engine, then keeps the Go side intentionally small:

- one stable wrapper ABI for every export
- native wasm code on an off-heap foreign stack
- mmap-backed linear memory exposed directly as `[]byte`
- optional precompiled `.wago` blobs for fast reloads

Current target: **linux/amd64**.

## Installation

CLI:

```bash
./wago.sh
```

Once the hosted installer is live:

```bash
curl -fsSL https://wago.sh | sh
```

Library:

```bash
go get github.com/wago-org/wago
```

From a checkout:

```bash
go build -o wago ./cli/wago
go install ./cli/wago
```

## Usage

### CLI

```bash
./wago run tests/testdata/fib.wasm 30
./wago run -e hypot tests/testdata/fprog.wasm 3.0 4.0

./wago compile -o /tmp/fib.wago tests/testdata/fib.wasm
./wago run /tmp/fib.wago 30

./wago profile tests/testdata/fib.wasm 30
./wago validate tests/testdata/fib.wasm
```

Arguments are typed from the target export signature. You can override a parsed
type with a suffix:

```bash
./wago run -e hypot tests/testdata/fprog.wasm 3:f64 4:f64
```

Replace the `tests/testdata/*.wasm` paths with your own module.

### Go API

```go
package main

import (
	"fmt"
	"os"

	"github.com/wago-org/wago"
)

func main() {
	wasmBytes, err := os.ReadFile("tests/testdata/fprog.wasm")
	if err != nil {
		panic(err)
	}

	out, err := wago.RunValues(wasmBytes, "hypot", wago.F64(3), wago.F64(4))
	if err != nil {
		panic(err)
	}
	fmt.Println(out[0].AsF64()) // 5
}
```

For repeated calls, compile and instantiate explicitly:

```go
wasmBytes, err := os.ReadFile("tests/testdata/fib.wasm")
if err != nil {
	panic(err)
}

c, err := wago.Compile(wasmBytes)
if err != nil {
	panic(err)
}

in, err := wago.Instantiate(c, nil)
if err != nil {
	panic(err)
}
defer in.Close()

out, err := in.Invoke("fib", wago.I32(30))
if err != nil {
	panic(err)
}
fmt.Println(out[0].AsI32())
```

## API

### `Value`

`Value` is the typed 8-byte call slot used for arguments and results.

```go
wago.I32(1)
wago.I64(1)
wago.F32(1.5)
wago.F64(1.5)
```

Read results with `AsI32`, `AsI64`, `AsF32`, or `AsF64`.

### `Compile` / `Load`

```go
c, err := wago.Compile(wasmBytes)
blob, err := c.MarshalBinary()

c, err = wago.Load(blob)      // precompiled .wago
c, err = wago.Load(wasmBytes) // raw wasm, compiled on load
```

Use `CompileTimed` when you want decode, validate, and compile timings.

### `Instance`

```go
in, err := wago.Instantiate(c, hosts)
defer in.Close()

mem := in.LinearMemory()
out, err := in.Invoke("exported", args...)
g, err := in.Global("exported_global")
err = in.SetGlobal("mutable_exported_global", wago.I32(42))
```

`LinearMemory` returns the same mmap-backed region native wasm code sees. Writes
are visible in both directions without copying.

`Global` and `SetGlobal` access exported numeric globals by name. Reads return the
declared value type and current bits. Writes require an exported mutable global
and a matching `Value` type.

### Host imports

Host imports are keyed by `"module.name"`:

```go
hosts := map[string]wago.HostFunc{
	"env.log": func(arg int32) {
		fmt.Println(arg)
	},
}

in, err := wago.Instantiate(c, hosts)
```

Current host function imports are void and receive the first `i32` argument.
Native code logs import calls, then Go dispatches them after the wasm call
returns.

Imported globals are supplied through `InstantiateWithImports`, or through the
one-shot `RunValuesWithImports` / `RunWithImports` helpers:

```go
counter := wago.NewGlobal(wago.I32(10), true)
defer counter.Close()

imports := wago.Imports{
	Funcs: hosts,
	Globals: map[string]wago.GlobalImport{
		"env.counter": {Global: counter},
	},
}

in, err := wago.InstantiateWithImports(c, imports)
out, err := wago.RunValuesWithImports(wasmBytes, imports, "get_counter")
```

Use `GlobalImport{Global: g}` for shared imported globals, especially mutable
ones. The instance stores that host-owned global cell directly: wasm writes,
`Instance.SetGlobal`, `g.Set`, and other instances importing the same `*Global`
all observe the same value. Call `g.Close()` only after every instance that uses
it has been closed.

For one-shot or immutable imports, `GlobalImport{Type, Mutable, Bits}` is a
convenience shorthand. `GlobalImport.Bits` uses the raw wasm numeric encoding:
`i32`/`f32` use the low 32 bits (integer bits or IEEE-754 f32 bits), and
`i64`/`f64` use all 64 bits (integer bits or IEEE-754 f64 bits). In this
shorthand form, wago creates the imported global object during instantiation;
mutating the original `GlobalImport` map after `InstantiateWithImports` returns
is not observed by the instance.

## Feature Support

`wago` runs real AssemblyScript modules across the core scalar types:

| Area | Status |
|---|---|
| Values | `i32`, `i64`, `f32`, `f64` arithmetic, compares, conversions, reinterpret |
| Control flow | `block`, `loop`, `if`, `else`, `br`, `br_if`, `br_table`, `return`, `select` |
| Memory | bounds-checked linear-memory loads/stores, checked active data segments |
| Globals | numeric immutable/mutable globals, global imports/exports, `Global`/`SetGlobal` accessors |
| Calls | direct calls, recursion, `call_indirect`, checked active element segments |
| Host imports | void/log-style imports, batched back to Go |
| Serialization | precompiled `.wago` blobs |

See [FEATURES.md](FEATURES.md) for the full matrix and [ROADMAP.md](ROADMAP.md)
for the plan.

Notable gaps today: `memory.grow`, start functions, bulk-memory ops, exact
float trunc traps / NaN min-max behavior, i64 sub-width loads, WASI, and
platforms beyond linux/amd64.

## Performance

The local `bench/` suite compares against wazero v1.9. On the development
machine used for this snapshot:

- compile is ~**34x faster**
- host-to-wasm call overhead is ~**3x lower**
- host-to-wasm calls allocate **0 bytes**
- loop execution is competitive
- recursion and instantiate currently trail wazero (see [ROADMAP.md](ROADMAP.md))

<img src="https://raw.githubusercontent.com/wago-org/docs/main/charts/speedup.svg" alt="wago speedup vs wazero" width="100%">

<img src="https://raw.githubusercontent.com/wago-org/docs/main/charts/latency.svg" alt="latency: ns/op, wago vs wazero" width="100%">

The charts live in the [`wago-org/docs`](https://github.com/wago-org/docs) repo
and are embedded here via raw URLs, so regenerating them never churns this repo's
history. Preview locally and publish:

```bash
cd bench && go run ./chart     # preview into bench/charts/ (gitignored)
./scripts/publish-charts.sh    # regenerate on a stable machine, push to wago-org/docs
```

`bench/` is a separate Go module so the root package stays dependency-light; the
chart generator is pure-Go SVG (no chart runtime).

## Architecture

The core path is:

```text
wasm bytes
  -> src/core/compiler/wasm        decode + validate
  -> src/core/compiler/backend     single-pass amd64 codegen
  -> src/core/runtime              W^X mmap + foreign-stack trampoline
```

The runtime calls every export through a single wrapper shape:

```text
WasmWrapper(serArgs, linMem, trap, results)
```

Arguments and results are 8-byte slots. `linMem` points at the mmap-backed
linear memory. Traps are reported through a small trap slot.

The backend uses a Valent-Block style symbolic operand stack: straight-line code
stays register-resident, while control-flow joins flush to deterministic frame
slots so every incoming edge agrees on machine state.

## Project Layout

```text
.
  wago.go                         public Go API
  cli/wago/                       CLI
  src/core/compiler/wasm/         decoder + validator
  src/core/compiler/backend/amd64/ single-pass x86-64 backend
  src/core/runtime/               no-cgo execution runtime
  tests/testdata/                 wasm fixtures
  bench/                          wazero comparison benchmarks
  warp/                           reference C++ WARP tree
```

## Running Tests

```bash
go test ./...

cd bench
go test ./...
go test -bench .
```

The wasm frontend can also run the official WebAssembly spec testsuite when
`WAGO_SPECTEST_DIR` points at a checkout and `wast2json` is on `PATH`.

## Contributing

This project is early and intentionally small. [ROADMAP.md](ROADMAP.md) has the
best list of useful work. Keep changes narrow, include regression tests, and
prefer the existing WARP-shaped layout over new abstractions.

## License

See [LICENSE](LICENSE).

The reference WARP tree under [warp/](warp/) keeps its original license headers.

## Contact

Open an issue or discussion on the project repository.
