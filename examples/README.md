# wago examples

Runnable, self-contained examples of the wago API — from running a single module
to building plugins, actors, and supervised process trees. Each example is a small
`main.go` you can run directly:

```sh
go run ./examples/01-hello
go run ./examples/12-actors
```

The tiny WebAssembly modules the examples run against are assembled in-process by
[`internal/mods`](internal/mods/mods.go) so nothing here needs an external wasm
toolchain — real projects would load `.wasm` files compiled from Rust,
AssemblyScript, TinyGo, or C instead. The examples are about the **wago Go API**,
not wasm authoring.

## Go API examples

| # | Example | Shows |
|---|---------|-------|
| 01 | [hello](01-hello) | Low-level `Compile` → `Instantiate` → `Invoke` |
| 02 | [runtime-typed](02-runtime-typed) | `Runtime` + context-aware typed `Call` with `Value` |
| 03 | [host-import](03-host-import) | Defining a `HostFunc` the guest calls back into |
| 04 | [memory](04-memory) | Reading/writing guest linear memory from the host |
| 05 | [globals](05-globals) | Reading and setting exported globals, typed |
| 06 | [plugin-timer](06-plugin-timer) | Using a built-in plugin (`timer`) |
| 07 | [plugins-log-metrics](07-plugins-log-metrics) | Combining plugins; reading metrics back host-side |
| 08 | [custom-plugin](08-custom-plugin) | Writing your own `Extension` |
| 09 | [capabilities-policy](09-capabilities-policy) | Capability/resource `Policy` enforcement |
| 10 | [hooks](10-hooks) | Invoke/compile hooks (tracing, auto-instrumentation) |
| 11 | [class-pool](11-class-pool) | `Class` + instance pool with reset (`Acquire`/`Release`) |
| 12 | [actors](12-actors) | Processes + mailboxes (`Spawn`/`Send`/`Monitor`) |
| 13 | [supervisor](13-supervisor) | Supervision trees with restart strategies |
| 14 | [handles](14-handles) | `HandleTable` resource handles with a generation guard |
| 15 | [config](15-config) | `RuntimeConfig`: features and bounds-check modes |
| 16 | [serialize](16-serialize) | Precompiling to a `.wago` blob and loading it |
| 17 | [per-call-imports](17-per-call-imports) | `WithImports` + the by-name plugin registry |

Run them all:

```sh
for d in examples/[0-9]*; do echo "== $d =="; go run "./$d"; done
```

## Host functions are always the stack form

Every host import — whether ad-hoc or provided by a plugin — is a `wago.HostFunc`:

```go
func(m wago.HostModule, params, results []uint64)
```

It reads its wasm arguments from `params` (decode with `wago.AsI32`/`AsI64`/…),
writes results into `results` (encode with `wago.I32`/…), and can reach the
calling instance's linear memory via `m.Memory()`. This one reflection-free form
binds identically on standard Go and TinyGo — see
[03-host-import](03-host-import) and [04-memory](04-memory).

## Writing a plugin

A plugin implements `wago.Extension` (`Info` + `Register`) and declares its host
imports, capabilities, and optional lifecycle hooks through the `Registry` —
see [08-custom-plugin](08-custom-plugin) and [10-hooks](10-hooks). Register it on
a runtime with `rt.Use(myplugin.Ext())`.

## CLI

The `wago` CLI mirrors much of this from the shell.

Run a module and inspect it:

```sh
wago run add.wasm 2 40                 # compile + execute (typed args)
wago run -e fib fib.wasm 30            # pick an export
wago run --plugin timer,log app.wasm   # enable compiled-in plugins
wago module imports app.wasm           # what a module imports (resolved vs plugins)
wago module capabilities app.wasm      # capabilities a module requires
```

Plugins compiled into the binary:

```sh
wago plugin list                       # plugins available in this binary
wago plugin inspect timer              # a plugin's imports, signatures, capabilities
```

Declare plugins for a custom build (a `wago-plugins.json` manifest):

```sh
wago plugin add timer                                        # a built-in
wago plugin add github.com/acme/wago-redis --version v0.3.1  # a third-party module
wago plugin manifest                                         # show the manifest
```

Version management (nvm-style; ships in every build, network install in full builds):

```sh
wago --version              # this binary's version
wago version list           # installed versions
wago version install 0.5.0  # download + verify (full build)
wago version use 0.5.0      # switch the active version
wago env                    # resolved config/cache/data directories
```

See the repository root for building the CLI (`make build`) and the lean,
TinyGo-compiled release (`make build-release`).
