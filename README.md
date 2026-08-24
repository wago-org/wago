<h1 align="center"><pre>╦ ╦ ╔═╗ ╔═╗ ╔═╗
║║║ ╠═╣ ║ ╦ ║ ║
╚╩╝ ╩ ╩ ╚═╝ ╚═╝</pre></h1>

<p align="center">
  A pure-Go WebAssembly runtime and native compiler.
</p>

<p align="center">
  <a href="https://github.com/wago-org/wago/actions/workflows/ci.yml"><img src="https://github.com/wago-org/wago/actions/workflows/ci.yml/badge.svg" alt="CI"></a>
  <a href="https://go.dev/"><img src="https://img.shields.io/badge/go-%3E%3D1.22-00ADD8.svg" alt="Go >= 1.22"></a>
  <a href="https://github.com/wago-org/wago/releases"><img src="https://img.shields.io/github/v/release/wago-org/wago?include_prereleases&label=release" alt="Release"></a>
  <a href="LICENSE"><img src="https://img.shields.io/github/license/wago-org/wago" alt="License"></a>
</p>

<p align="center">
  <a href="https://docs.wago.sh">Documentation</a> ·
  <a href="https://docs.wago.sh/getting-started">Getting started</a> ·
  <a href="https://plugins.wago.sh">Plugins</a> ·
  <a href="https://wago.sh/#performance">Benchmarks</a>
</p>

Wago decodes, validates, and compiles WebAssembly directly to machine code in a
single pass. It has no interpreter, cgo dependency, or C toolchain.

Use it from the CLI or embed it as a Go library. Wago can save
architecture-specific `.wago` artifacts, build standalone executables, and add
host capabilities such as WASI and the Component Model through plugins.

> [!WARNING]
> Wago is pre-release. APIs and native artifact formats may change.

## Install

macOS and Linux:

```sh
curl -fsSL https://install.wago.sh/unix | sh
wago version install
```

Windows PowerShell:

```powershell
irm https://install.wago.sh/ps | iex
wago version install
```

Other installation methods and release channels are covered in
[Getting started](https://docs.wago.sh/getting-started).

## Run

```sh
curl -fsSL https://wago.sh/corpora/fib.wasm -o fib.wasm
wago fib.wasm 30
```

```text
fib(30) = 832040
```

Precompile the module or build a standalone executable:

```sh
wago build fib.wasm -o fib.wago
wago compile --invoke fib fib.wasm -o fib
./fib 30
```

## Go API

```sh
go get github.com/wago-org/wago
go run github.com/wago-org/wago/examples/02-runtime-typed@latest
```

See [Embed Wago in Go](https://docs.wago.sh/guides/embed-wago).

## Support

Wago's native runtime supports Linux, macOS, and Windows on amd64 and arm64.
Exact WebAssembly feature coverage varies by backend; see
[FEATURES.md](FEATURES.md).

## Documentation

**Use:** [Run modules](https://docs.wago.sh/guides/run-a-module) ·
[Embed in Go](https://docs.wago.sh/guides/embed-wago) ·
[Host functions](https://docs.wago.sh/guides/host-functions) ·
[Plugins](https://docs.wago.sh/guides/plugins)

**Reference:** [Examples](examples/README.md) · [Features](FEATURES.md) ·
[Architecture](ARCHITECTURE.md) · [Benchmarks](bench/README.md) ·
[Roadmap](ROADMAP.md)

**Project:** [Contributing](CONTRIBUTING.md) ·
[Issues](https://github.com/wago-org/wago/issues)

## License

[Apache License 2.0](LICENSE)
