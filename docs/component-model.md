# Component Model runtime

The official [`wago-org/component-model`](https://github.com/wago-org/component-model)
plugin decodes and instantiates WebAssembly Components on Wago. It supports the
Preview 2 binary model, canonical ABI lift/lower, typed values, resources, nested
composition, and typed host imports. The plugin also contains experimental Preview 3
task, future, and stream machinery without making those features ambient in core Wago.

Component execution is an opt-in plugin, not an ambient `Runtime` feature:

```go
core := wago.NewRuntime()
defer core.Close()

components, err := component.Enable(core)
if err != nil {
	return err
}
instance, err := components.Instantiate(ctx, componentBytes, opts...)
```

`component.Enable` loads the registered `wago-org/component-model` extension with
the `runtime.core` plugin capability. Manifest-driven hosts can select the same
plugin by ID and must explicitly grant that capability. The compatibility
`component.Instantiate(ctx, core, ...)` entry point resolves the installed plugin
and fails if it has not been enabled.

The plugin provides `wago-org/component-model/runtime/v1` as a typed service.
WASI and other component-world plugins require that service, so the plugin plan
orders them without either consumer receiving core-engine authority.

## Trust boundary

The component runtime is a linker and execution engine. It does not grant WASI
authority by itself. Filesystem, network, clock, random, and HTTP policy belongs
in a host package such as [`wago-org/wasi`](https://github.com/wago-org/wasi).
Hosts should expose capabilities explicitly and keep them denied by default.

`runtime.core` is deliberately privileged but narrow: it permits a trusted
execution-model plugin to compile and instantiate embedded core modules and own
typed host-function references, without exposing plugin registration, policy,
inspection, or arbitrary runtime lifecycle control.
Ordinary plugins should use narrower host-import, hook, or managed-instance
capabilities. The runtime revokes the Component Model plugin's handle during
shutdown, and later component instantiation fails closed.

Component decoding and canonical ABI operations reject malformed encodings,
invalid type relationships, out-of-bounds memory access, invalid resource
ownership transfers, and unsupported behavior. Missing host imports resolve to
named trap stubs so an unimplemented operation fails at its call site.

## Host re-entry

Preview 1 adapters embedded in Preview 2 components can call host functions
through imported tables and synchronously re-enter a parked instance. Wago gives
each nested activation an isolated native stack, control frame, trap cell, and
call scratch, then restores the outer activation in LIFO order. Host code can
use `Instance.InvokeFromHost` when it needs an explicit callback entry and
`HostTrap` to return a host error through the native trap boundary.

Use `wago.WithSynchronousHostCalls()` when instantiating a core adapter module
whose host functions may be reached only indirectly through a table.

## Validation

The external plugin repository includes decoder, canonical ABI, resource,
composition, async, oracle, and malformed-input tests. A real Rust
`wasm32-wasip2` `wasi:cli/command` fixture is exercised by the
[`wago-org/wasi`](https://github.com/wago-org/wasi) host module.
