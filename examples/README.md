# wago examples

These runnable, self-contained programs teach the wago Go API. Start with one
module, then move to host functions, memory, and plugins. Each example has a
small `main.go` that you can run from the repository root:

```sh
go run ./examples/01-hello
go run ./examples/10-hooks
```

[`internal/mods`](internal/mods/mods.go) builds the small WebAssembly modules in
process. You do not need a separate wasm toolchain to run these examples. Real
projects normally load `.wasm` files built by Rust, AssemblyScript, TinyGo, or
C. These examples teach the wago Go API, not wasm authoring.

## Go API examples

| # | Example | Shows |
|---|---------|-------|
| 01 | [hello](01-hello) | Low-level `Compile`, `Instantiate`, then `Invoke` |
| 02 | [runtime-typed](02-runtime-typed) | `Runtime` and context-aware typed `Call` with `Value` |
| 03 | [host-import](03-host-import) | Define a `HostFunc` that guest code calls |
| 04 | [memory](04-memory) | Read and write guest linear memory from the host |
| 05 | [globals](05-globals) | Read and set typed exported globals |
| 08 | [custom-plugin](08-custom-plugin) | Write an explicit `PluginProvider` |
| 10 | [hooks](10-hooks) | Add invoke and compile hooks for tracing or instrumentation |
| 14 | [handles](14-handles) | Use generation-checked `HandleTable` resource handles |
| 15 | [config](15-config) | Configure features, bounds checks, and function workers |
| 16 | [serialize](16-serialize) | Precompile and load a `.wago` blob |

Run them all:

```sh
for d in examples/[0-9]*; do echo "== $d =="; go run "./$d"; done
```

## Write a Host Function

Every host import, including a plugin import, uses this `wago.HostFunc` form:

```go
func(m wago.HostModule, params, results []uint64)
```

Read wasm arguments from `params`, with helpers such as `wago.AsI32` and
`wago.AsI64`. Write results to `results`, with helpers such as `wago.I32`. Use
`m.Memory()` to access the calling instance's linear memory.

This reflection-free form works the same way in standard Go and TinyGo. See
[03-host-import](03-host-import) and [04-memory](04-memory).

## Writing a plugin

A plugin implements the one-method `wago.Plugin` interface. It has an immutable
`PluginDefinition` and a factory in an explicit `PluginProvider`.

The plugin declares host imports, guest capabilities, lifecycle observation, and
the exact privileged Authorities it needs. The host reviews those Authorities in
a `PluginSelection`. It then loads the full `PluginSet` atomically with
`rt.LoadPlugins`. See [08-custom-plugin](08-custom-plugin) and
[10-hooks](10-hooks).

## CLI

The `wago` CLI provides many of the same operations from the shell. Build it
with `make build` before you run these commands.

Run a module and inspect it:

```sh
wago run add.wasm 2 40                 # compile + execute (typed args)
wago run -e fib fib.wasm 30            # pick an export
wago add github.com/acme/wago-metrics  # review and lock the plugin first
wago run app.wasm
wago module imports app.wasm           # what a module imports (resolved vs plugins)
wago module capabilities app.wasm      # capabilities a module requires
```

Plugins compiled into the binary:

```sh
wago plugin list                       # plugins available in this binary
wago plugin inspect github.com/acme/wago-metrics
```

Declare plugins for a custom build (`wago.json` plus `wago-lock.json`):

```sh
wago add github.com/acme/wago-metrics             # a direct plugin requirement
wago add github.com/acme/wago-redis@0.3.1          # an exact release
wago plugin tree                                  # direct and transitive graph
wago plugin list --json                           # linked definitions and plan
```

Version management is similar to nvm. It ships in every build; network install
is available in full builds:

```sh
wago --version              # this binary's version
wago version list           # installed versions
wago version install 0.5.0  # download + verify (full build)
wago version use 0.5.0      # switch the active version
wago env                    # resolved config/cache/data directories
```

See the repository root for the standard CLI build (`make build`) and the lean
TinyGo release build (`make build-release`).
