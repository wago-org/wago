<h1 align="center"><pre>╦ ╦ ╔═╗ ╔═╗ ╔═╗
║║║ ╠═╣ ║ ╦ ║ ║
╚╩╝ ╩ ╩ ╚═╝ ╚═╝</pre></h1>

<p align="center">
  A fast, compact WebAssembly runtime for Go
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

Wago is a WebAssembly runtime for Go. It compiles Wasm to native machine code
and runs it without cgo, a C toolchain, or an interpreter.

Use the CLI to run `.wasm` files, save precompiled `.wago` artifacts, or create
standalone executables. Use the Go package to embed the same runtime in an
application. Plugins provide host integrations such as WASI and the Component
Model.

> [!NOTE]
> Wago is still pre-release. APIs and `.wago` artifacts may change.

## Choose a path

- **Run a Wasm module:** [Install Wago](#install), then
  [run a module](#run-a-module).
- **Embed Wago in Go:** go to [Use Wago from Go](#use-wago-from-go).
- **Add WASI or another host integration:** go to
  [Add host capabilities](#add-host-capabilities).
- **Learn the implementation:** see [Architecture](ARCHITECTURE.md) and the
  [feature matrix](FEATURES.md).

## Install

On macOS and Linux:

```sh
curl -fsSL https://install.wago.sh/unix | sh
wago version install
```

On Windows, in PowerShell:

```powershell
irm https://install.wago.sh/ps | iex
wago version install
```

These commands install the Wago manager and then install a runtime. See
[Getting started](https://docs.wago.sh/getting-started) for other installation
methods, release channels, and source builds.

## Run a module

Download and run a small Fibonacci module:

```sh
curl -fsSL https://wago.sh/corpora/fib.wasm -o fib.wasm
wago fib.wasm 30
```

```text
fib(30) = 832040
```

Save it as a precompiled artifact:

```sh
wago build fib.wasm -o fib.wago
wago fib.wago 30
```

Or build a standalone executable:

```sh
wago compile --invoke fib fib.wasm -o fib
./fib 30
```

## Use Wago from Go

Add Wago to your module:

```sh
go get github.com/wago-org/wago
```

Run the small typed API example:

```sh
go run github.com/wago-org/wago/examples/02-runtime-typed@latest
```

The example compiles a module, creates an instance, and calls an exported
function. See [Embed Wago in Go](https://docs.wago.sh/guides/embed-wago) for a
complete guide.

## Add host capabilities

Wago keeps host integrations outside the core runtime. For example, add WASI to
a project with:

```sh
wago init
wago add wago-org/wasi
```

See [Use plugins](https://docs.wago.sh/guides/plugins) or browse the
[plugin registry](https://plugins.wago.sh).

## Platforms

Wago supports Linux, macOS, and Windows on amd64 and arm64. WebAssembly feature
support varies by backend. See the [feature matrix](FEATURES.md) for exact
coverage.

## Learn more

Most usage and implementation details live in the
[documentation](https://docs.wago.sh).

[Examples](examples/README.md) · [Features](FEATURES.md) ·
[Architecture](ARCHITECTURE.md) · [Benchmarks](bench/README.md) ·
[Roadmap](ROADMAP.md) · [Contributing](CONTRIBUTING.md) ·
[Issues](https://github.com/wago-org/wago/issues)

## License

[Apache License 2.0](LICENSE)
