<h1 align="center"><pre>╦ ╦ ╔═╗ ╔═╗ ╔═╗
║║║ ╠═╣ ║ ╦ ║ ║
╚╩╝ ╩ ╩ ╚═╝ ╚═╝</pre></h1>

<p align="center">
  A wonderfully quick, compact, and extensible WebAssembly runtime for Go
</p>

<p align="center">
  <a href="https://github.com/wago-org/wago/actions/workflows/ci.yml"><img src="https://github.com/wago-org/wago/actions/workflows/ci.yml/badge.svg" alt="CI"></a>
  <a href="https://go.dev/"><img src="https://img.shields.io/badge/go-%3E%3D1.22-00ADD8.svg" alt="Go >= 1.22"></a>
  <a href="https://github.com/wago-org/wago/releases"><img src="https://img.shields.io/github/v/release/wago-org/wago?include_prereleases&label=release" alt="Release"></a>
  <a href="LICENSE"><img src="https://img.shields.io/github/license/wago-org/wago" alt="License"></a>
</p>

## Install

```sh
curl -fsSL https://wago.sh/install.sh | sh
```

On Windows, run this from Command Prompt. The installer provides the same
interactive install-directory, reinstall, progress, and PATH flow without
requiring script-execution policy changes. Both bootstrap scripts download the
same checksummed Go installer for their platform, then hand off the complete
installation flow. Go is only needed when a release manager is unavailable and
must be built from source:

```bat
curl.exe -fsSLo "%TEMP%\wago-install.cmd" https://raw.githubusercontent.com/wago-org/wago/main/install.cmd && call "%TEMP%\wago-install.cmd" && del "%TEMP%\wago-install.cmd"
```

For the Go package:

```sh
go get github.com/wago-org/wago
```

Read [Getting started](https://docs.wago.sh/getting-started) for installation
details, release channels, profiles, and source builds.

## Getting Started

Download a small module and run an exported function:

```sh
curl -fsSLo fib.wasm \
  https://raw.githubusercontent.com/wago-org/wago/main/tests/fixtures/wasm/fib.wasm
wago run --invoke fib fib.wasm 20
```

```text
fib(20) = 6765
```

`run` is the default command:

```sh
wago fib.wasm 20
wago run --core 3 --invoke main generated-wasmgc.wasm
```

The CLI preserves the WebAssembly 2 compatibility default. Pass `--core 3` to
compile and execute modules with the complete opt-in `CoreFeaturesV3` set.

Validate or precompile a module:

```sh
wago validate fib.wasm
wago build fib.wasm -o fib.wago
wago run --invoke fib fib.wago 20
```

Inspect its host requirements:

```sh
wago module imports fib.wasm
wago module capabilities fib.wasm
```

See [Run a module](https://docs.wago.sh/guides/run-a-module) for typed arguments,
watch mode, parallel compilation, `.wago` artifacts, and runtime flags.

### Run WASI

Create a project and add the WASI plugin:

```sh
wago init --run
wago add wago-org/wasi
wago run program.wasm hello world
```

`wago add` writes the plugin to `wago.json`, pins its resolved version and
reviewed capabilities in `wago-lock.json`, and rebuilds the selected runtime.

Useful plugin commands:

```sh
wago plugin list
wago plugin inspect
wago plugin grant
wago plugin update
wago plugin remove wago-org/wasi
```

Browse packages at [plugins.wago.sh](https://plugins.wago.sh). Read
[Use plugins](https://docs.wago.sh/guides/plugins) for local and global scopes,
capabilities, lockfiles, and publishing.

## Go API

Compile a module, create an instance, and call an export:

```go
package main

import (
	"context"
	"fmt"
	"os"

	"github.com/wago-org/wago"
)

func main() {
	wasmBytes, err := os.ReadFile("fib.wasm")
	if err != nil {
		panic(err)
	}

	rt := wago.NewRuntime()
	defer rt.Close()

	mod, err := rt.Compile(wasmBytes)
	if err != nil {
		panic(err)
	}

	ctx := context.Background()
	inst, err := rt.Instantiate(ctx, mod)
	if err != nil {
		panic(err)
	}
	defer inst.Close()

	out, err := inst.Call(ctx, "fib", wago.ValueI32(20))
	if err != nil {
		panic(err)
	}

	fmt.Println(out[0].I32()) // 6765
}
```

Read [Embed Wago in Go](https://docs.wago.sh/guides/embed-wago) for compilation,
instances, typed values, memory, cancellation, and cleanup.

### Host Functions

Host functions use one reflection-free stack form under standard Go and TinyGo:

```go
mul := wago.HostFunc(func(_ wago.HostModule, params, results []uint64) {
	a := wago.AsI32(params[0])
	b := wago.AsI32(params[1])
	results[0] = wago.I32(a * b)
})

inst, err := rt.Instantiate(context.Background(), mod, wago.WithImports(wago.Imports{
	"host.mul": mul,
}))
```

`HostModule` gives the function access to the calling instance and its linear
memory.

Read [Host functions](https://docs.wago.sh/guides/host-functions) for signatures,
memory access, errors, and capability policy. More runnable programs live in
[`examples/`](examples/README.md).

## More Examples

```sh
# Pick or update a runtime
wago version install canary
wago version switch
wago update

# Work with project plugins
wago init
wago add wago-org/wasi wago-org/workers
wago plugin outdated
wago plugin tree

# Inspect and maintain Wago
wago status
wago config
wago cache size
wago cache clean

# Use Wago from scripts or agents
wago --no-input version install --canary \
  --profile standard --build normal --use
wago update --all --no-input --dry-run --json
wago commands --json
```

Every interactive workflow has flags for one-shot use. Read the
[configuration reference](https://docs.wago.sh/reference/configuration) or run
`wago <command> --help` for the exact options.

## Performance

Wago compiles validated functions directly to native code in a single pass. The
published benchmark suite compares Wago and wazero within each architecture on
the same modules and machine.

These numbers come from the published July 30, 2026 snapshot at Wago
[`ff87ac3`](https://github.com/wago-org/wago/commit/ff87ac3a5868ebe074f06bf91ec61ac60c600924).
Architectures were measured on different machines, so compare values across a
row, not between the amd64 and arm64 tables.

### amd64

| Benchmark | Wago | wazero |
| --- | ---: | ---: |
| Compile `fib_rec` | 4 µs | 44.9 µs |
| Instantiate `fib_rec` | 2.1 µs | 8.7 µs |
| Host → Wasm call | 10.4 ns | 33.1 ns |
| Wasm → host → Wasm | 148.5 ns | 440.4 ns |
| Execute recursive fib | 370 µs | 538 µs |
| JSON deserialize | 40 µs | 63.6 µs |
| Execute ray tracer | 401 µs | 367 µs |
| Compile esbuild, Go heap | 74.2 MB | 256.2 MB |

### arm64

| Benchmark | Wago | wazero |
| --- | ---: | ---: |
| Compile `fib_rec` | 3.3 µs | 25.6 µs |
| Instantiate `fib_rec` | 550.4 ns | 8.9 µs |
| Host → Wasm call | 18.7 ns | 23.9 ns |
| Wasm → host → Wasm | 119.9 ns | 315.5 ns |
| Execute recursive fib | 260 µs | 387 µs |
| JSON deserialize | 40.4 µs | 44 µs |
| Execute ray tracer | 275 µs | 255 µs |
| Compile esbuild, Go heap | 82.2 MB | 262.7 MB |

Go heap rows exclude guest linear memory, native code mappings, virtual-memory
reservations, RSS, and PSS.

### Whole-Process Startup

Spawn-to-exit wall-clock latency, including process startup, compilation,
instantiation, and execution:

| Workload | Wago | Closest result |
| --- | ---: | ---: |
| json-as | 5.1 ms | wasmi 7.4 ms |
| nbody | 30.9 ms | wasmtime 30.1 ms |
| spectral norm | 3.7 ms | wazero 4.8 ms |
| quicksort | 9.4 ms | wasmtime 10.8 ms |
| ray tracer | 9.0 ms | wazero 8.8 ms |
| SHA-256 | 2.4 ms | wazero 2.9 ms |

See [wago.sh/#performance](https://wago.sh/#performance) for the full tables.
The benchmark corpus and methodology are in [`bench/`](bench/README.md).

Run them locally:

```sh
cd bench
go test -bench . -benchmem
```

## Support

Wago supports the WebAssembly 1.0 core language and the implemented
WebAssembly 2.0 release features, including multi-value, reference types, bulk
memory, extended constant expressions, and SIMD. Linux amd64 is the primary
target; Linux arm64 and macOS arm64 have native runtime coverage.

Wago is JIT-only. Unsupported or disabled features fail during decode or
validation.

Read the [Wago documentation](https://docs.wago.sh) for guides and reference
material. The repository also keeps the detailed [feature matrix](FEATURES.md),
[architecture notes](ARCHITECTURE.md), and [roadmap](ROADMAP.md).

## Development

```sh
make lint
make test
make test-guard
```

Run `make` to list build, test, conformance, benchmark, and release targets.
Read [CONTRIBUTING.md](CONTRIBUTING.md) before opening a pull request.

## License

Wago is distributed under the [Apache License 2.0](LICENSE).

## Contact

- [Documentation](https://docs.wago.sh)
- [Issues](https://github.com/wago-org/wago/issues)
- [Plugin registry](https://plugins.wago.sh)
- [Website](https://wago.sh)
