<p align="center">
  <a href="https://wago.sh">
    <img src="https://wago.sh/assets/wago-logo.png" width="104" height="104" alt="wago logo">
  </a>
</p>

<h1 align="center">wago</h1>

<p align="center">
  A WebAssembly engine, written in pure Go.
</p>

<p align="center">
  <a href="https://wago.sh">Website</a>
  ·
  <a href="FEATURES.md">Features</a>
  ·
  <a href="ROADMAP.md">Roadmap</a>
  ·
  <a href="ARCHITECTURE.md">Architecture</a>
  ·
  <a href="bench/README.md">Benchmarks</a>
</p>

<pre align="center">
╦ ╦ ╔═╗ ╔═╗ ╔═╗
║║║ ╠═╣ ║ ╦ ║ ║
╚╩╝ ╩ ╩ ╚═╝ ╚═╝
</pre>

`wago` decodes, validates, compiles, and runs WebAssembly through a no-cgo
single-pass x86-64 JIT. It is built for Go programs that want native wasm calls
without a C toolchain, and for small systems where startup time, memory shape,
and operational simplicity matter.

Current target: **linux/amd64**.

## Install

CLI:

```bash
curl -fsSL https://wago.sh/install.sh | sh
```

During private development this builds from source over SSH, so you need read
access to `git@github.com:wago-org/wago` and Go 1.22+. The same installer is the
public entry point for v0.1.0.

Useful installer knobs:

```bash
WAGO_VERSION=main        # branch, tag, or commit
WAGO_BIN_DIR=~/.local/bin
WAGO_DRY_RUN=1
```

Go library:

```bash
go get github.com/wago-org/wago
```

From a checkout:

```bash
go build -o wago ./cli/wago
go install ./cli/wago
```

## Try It

Run a wasm export:

```bash
wago run tests/testdata/fib.wasm 30
wago run -e hypot tests/testdata/fprog.wasm 3:f64 4:f64
```

Validate a module:

```bash
wago validate tests/testdata/fib.wasm
```

Arguments are typed from the export signature. Add a suffix when the literal
needs a precise wasm type: `42`, `7:i64`, `3.5:f64`.

## Embed It

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

	mod, err := wago.Compile(wasmBytes)
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
	fmt.Println(wago.AsF64(out[0]))
}
```

Arguments and results use raw 8-byte wasm call slots:

```go
wago.I32(1)
wago.I64(1)
wago.F32(1.5)
wago.F64(1.5)
```

Read results with `AsI32`, `AsI64`, `AsF32`, or `AsF64`.

## What Ships

| Area | Status |
|---|---|
| WebAssembly 1.0 MVP | Complete; pinned pre-reference-types spectest passes in full |
| Values | `i32`, `i64`, `f32`, `f64`, conversions, reinterpret, trunc traps |
| Control flow | `block`, `loop`, `if`, `else`, branches, `select`, recursion |
| Calls | direct calls, `call_indirect`, host imports, cross-instance function links |
| Memory | all scalar load/store widths, `memory.size`, `memory.grow`, `memory.copy`, `memory.fill` |
| Imports/exports | functions, memories, tables, globals, mutable shared globals |
| Runtime | W^X mmap code, foreign stack, trap-to-error path, zero-copy linear memory |
| Platform | linux/amd64 today; more targets planned |

See [FEATURES.md](FEATURES.md) for the full support matrix and
[ROADMAP.md](ROADMAP.md) for planned work.

## Why Wago

- **No cgo.** Build and deploy with the Go toolchain only.
- **JIT-only.** Wasm compiles to native x86-64 code; there is no interpreter tier.
- **Small runtime shape.** Linear memory is mmap-backed and exposed directly as
  `[]byte`.
- **Fast startup path.** The compiler is single-pass and consumes validated wasm
  bytes directly.
- **Auditable backend.** The railshot backend follows WARP's Valent-Block style:
  register-resident straight-line code with deterministic frame slots at joins.

## Performance

The benchmark suite compares wago against wazero over the same wasm corpus. The
current project snapshot reports:

| Workload | Direction |
|---|---|
| Compile latency | about 34x faster than wazero |
| Host-to-wasm calls | lower overhead, 0 B/op on the hot call path |
| Scalar loops and memory kernels | competitive to faster on several corpus cases |
| Recursion and some larger real-world kernels | still active optimization targets |

Run the local suite:

```bash
cd bench
go test ./...
go test -bench .
```

The methodology and chart publishing flow live in [bench/README.md](bench/README.md).

## Architecture

```text
wasm bytes
  -> decode + validate
  -> byte-backed module body
  -> railshot single-pass x86-64 codegen
  -> no-cgo runtime: mmap code, foreign stack, trap cell, linear memory
```

For the full design, including ABI layout, memory/trap handling, globals, host
imports, and conformance strategy, read [ARCHITECTURE.md](ARCHITECTURE.md).

## Project Map

```text
cli/wago/                         CLI
src/wago/                         public API implementation
wago.go                           root package facade
src/core/compiler/wasm/           decoder and validator
src/core/compiler/backend/railshot/ single-pass x86-64 backend
src/core/runtime/                 no-cgo execution runtime
bench/                            corpus and comparison benchmarks
tests/testdata/                   small wasm fixtures
docs/                             design notes and active plans
```

## Development

```bash
go test ./...

cd bench
go test ./...
```

Optional checks:

```bash
make spectest      # requires WAGO_SPECTEST_DIR and wast2json
make tinygo-build  # requires TinyGo + lld
make bench-warp    # requires cmake + a C++14 toolchain
```

Contributors should start with [CONTRIBUTING.md](CONTRIBUTING.md), then check
[ROADMAP.md](ROADMAP.md) before changing feature support or priorities.

## License

Apache-2.0. See [LICENSE](LICENSE).

The reference WARP tree under [warp/](warp/) keeps its original license headers.
