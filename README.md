<h1 align="center"><pre>╦ ╦ ╔═╗ ╔═╗ ╔═╗
║║║ ╠═╣ ║ ╦ ║ ║
╚╩╝ ╩ ╩ ╚═╝ ╚═╝</pre></h1>

<p align="center">
  a wonderfully quick, compact, and extensible webassembly runtime for go
</p>

<p align="center">
  <a href="https://github.com/wago-org/wago/actions/workflows/ci.yml"><img src="https://github.com/wago-org/wago/actions/workflows/ci.yml/badge.svg" alt="CI"></a>
  <a href="https://go.dev/"><img src="https://img.shields.io/badge/go-%3E%3D1.22-00ADD8.svg" alt="Go >= 1.22"></a>
  <a href="https://github.com/wago-org/wago/releases"><img src="https://img.shields.io/github/v/release/wago-org/wago?include_prereleases&label=release" alt="Release"></a>
  <a href="LICENSE"><img src="https://img.shields.io/github/license/wago-org/wago" alt="License"></a>
  <a href="https://wago.sh/discord"><img src="https://img.shields.io/badge/discord-join-5865F2?logo=discord&logoColor=white" alt="Discord"></a>
</p>

<p align="center">
  <a href="https://docs.wago.sh">documentation</a> ·
  <a href="https://docs.wago.sh/getting-started">getting started</a> ·
  <a href="https://plugins.wago.sh">plugins</a> ·
  <a href="https://wago.sh/#performance">benchmarks</a> ·
  <a href="https://wago.sh/discord">discord</a> ·
  <a href="https://github.com/sponsors/JairusSW">sponsor us</a>
</p>

Wago is a pure-Go WebAssembly engine that compiles Wasm directly to native
machine code.

> [!NOTE]
> Wago is still beta. APIs and `.wago` artifacts may change.

## Why Wago?

* **Fast and lightweight.** Using a few *kilobytes* of ram, we compile _5x faster_ and execute _40% quicker_ than wazero.
* **Pure Go.** Embed Wago without CGO while keeping straightforward Go builds
  and cross-compilation.
* **Standards Compliant** We pass the [official WebAssembly test suite](https://github.com/WebAssembly/testsuite), millions of fuzzes, and many real-world corpora.
* **Extensible by design.** WASI, the Component Model, and other host
  capabilities live outside the core runtime as plugins.
* **Standalone executables** Compile your `.wasm` to _tiny_ native executables. Great for CLIs.

## Install

On macOS and Linux:

```sh
curl -fsSL https://install.wago.sh/unix | sh
```

On Windows, in PowerShell:

```powershell
irm https://install.wago.sh/ps | iex
```

Or run the installer with Go:

```sh
go run github.com/wago-org/wago/cli/wago-installer@latest
```

To keep the installer command:

```sh
go install github.com/wago-org/wago/cli/wago-installer@latest
wago-installer
```

These commands install the Wago manager. Run `wago version install` to install
a runtime. See [Getting started](https://docs.wago.sh/getting-started) for other
installation methods, release channels, and source builds.

## Run a module

Download and run a small Fibonacci module:

```sh
curl -fsSL https://wago.sh/corpora/fib.wasm -o fib.wasm
wago fib.wasm 30
```

```text
832040
```

Build a standalone executable:

```sh
wago compile fib.wasm -o fib
./fib 30
```
> [!NOTE]
> If you have any questions or want to invest in the community, please join our [Discord](https://wago.sh/discord)

## Use Wago from Go

Add Wago to your module:

```sh
go get github.com/wago-org/wago
```

Run the typed API example:

```sh
go run github.com/wago-org/wago/examples/02-runtime-typed@latest
```

The example compiles a module, creates an instance, and calls an exported
function. See [Embed Wago in Go](https://docs.wago.sh/guides/embed-wago) for the
complete guide.

## Performance

[**View the benchmarks →**](https://wago.sh/#performance)

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
support varies by backend and host.

See the [feature matrix](FEATURES.md) for exact coverage.

## Learn more

> 💖 We develop wago free-of-charge. If you or your company benefits from using wago or you'd like developement to continue, *please* consider sponsoring us! It'd truly mean the world

We use the [Apache License 2.0](LICENSE) so you can enjoy wago too!

[Documentation](https://docs.wago.sh) ·
[Examples](examples/README.md) ·
[Features](FEATURES.md) ·
[Architecture](ARCHITECTURE.md) ·
[Benchmarks](bench/README.md) ·
[Roadmap](ROADMAP.md) ·
[Contributing](CONTRIBUTING.md) ·
[Issues](https://github.com/wago-org/wago/issues) ·
[Sponsor us!](https://github.com/sponsors/JairusSW)
