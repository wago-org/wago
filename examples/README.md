# Wago examples

Each directory is a small program you can run from the repository root:

```sh
go run ./examples/01-hello
```

The matching test runs the same program. `go test ./examples/...` checks the full set.

Most examples build their tiny WebAssembly modules in process through [`internal/mods`](internal/mods/mods.go), so they need no guest toolchain. Real projects usually load `.wasm` files produced by Rust, AssemblyScript, TinyGo, or C.

## Start with the runtime

- [01 hello](01-hello) compiles, instantiates, and invokes with the low-level API.
- [02 typed runtime](02-runtime-typed) introduces `Runtime`, `Call`, `Value`, and cancellation.
- [03 host import](03-host-import) lets Wasm call a Go function.
- [04 memory](04-memory) reads and writes guest linear memory.
- [05 globals](05-globals) reads and sets an exported global.
- [06 runtime service](06-runtime-service) compiles once and uses isolated instances for concurrent requests.
- [07 runtime limits](07-runtime-limits) caps live instances and reuses the budget after close.
- [14 handles](14-handles) gives guests generation-checked handles to host resources.
- [15 runtime config](15-config) selects features, bounds checks, and compiler workers.
- [16 serialize](16-serialize) saves and loads a trusted compiled artifact.

## Write a host function

Every host import uses the same reflection-free function shape in standard Go and TinyGo:

```go
func(module wago.HostModule, params, results []uint64)
```

Read arguments from `params`, write results to `results`, and use `module.Memory()` when the call needs memory 0. Start with [03 host import](03-host-import), then [04 memory](04-memory).

## Write plugins

Read these in order when you are new to the plugin API.

- [08 custom plugin](08-custom-plugin) starts with a definition, provider, Authority, guest capability, and host import.
- [09 config and lifecycle](09-plugin-config-lifecycle) adds JSON configuration, semantic validation, guest arguments, `Start`, and `Stop`.
- [10 hooks](10-hooks) covers runtime, module, instance, and invocation interceptors and observers.
- [11 source transform](11-source-transform) changes Wasm bytes before compilation and observes the result.
- [12 caller context](12-caller-context) reads call cancellation and synchronously re-enters the active guest.
- [13 Contracts](13-plugin-contracts) connects two plugins through a typed, leased service.
- [17 managed instances](17-managed-instances) owns Wasm workers within reviewed instance and memory limits.
- [18 custom instruction](18-custom-instruction) defines portable semantics and a scalar compiler lowering.
- [19 custom type](19-custom-type) carries an expression-scoped 256-bit value through ordinary Wasm `externref`.
- [20 core handles](20-core-handles) compiles, instantiates, and creates a function reference during plugin startup.
- [21 guest storage](21-guest-storage) borrows checked linear memory inside a host callback.

The plugin examples use `examples/internal/exampleplugin` to build reviewed `PluginSet` values without repeating lockfile setup. Applications should load selections and grants produced by the CLI.

## Compare guest languages

[22 language guests](22-language-guests) calls the same `tutorial.answer() -> i32` host import from:

- [WebAssembly text](22-language-guests/wat/answer.wat)
- [AssemblyScript](22-language-guests/assemblyscript/answer.ts)
- [TinyGo](22-language-guests/tinygo/main.go)

Compiled `.wasm` files are checked in so the Go example has no extra build dependency. Run `./examples/22-language-guests/build.sh` to rebuild all three.

`HostModule.Memory()` remains the shortest path for memory 0. Use `GuestStorageHostModule` when the ABI needs indexed memory, Memory64 metadata, exact GC types, or Wasm GC arrays.
