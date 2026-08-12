# Component Model runtime

The official
[`github.com/wago-org/component-model`](https://github.com/wago-org/component-model)
plugin decodes and instantiates WebAssembly Components on Wago. It supports the
Preview 2 binary model, canonical ABI lift/lower, typed values, resources,
nested composition, and typed host imports. It also contains experimental
Preview 3 task, future, and stream machinery without putting those features in
core Wago.

Component execution is an opt-in plugin, not an ambient `Runtime` feature. A
generated host explicitly links `component-model/register.Providers()` and
activates the reviewed `github.com/wago-org/component-model` selection.
Consumers call it through its typed Contract:

```go
err := componentsRef.With(func(components component.Service) error {
    return components.WithInstance(ctx, componentBytes, func(instance *component.Instance) error {
        // Lift values, call exports, and lower results here. The service closes
        // the component instance before this callback-scoped operation returns.
        return nil
    }, opts...)
})
```

The plugin provides the
`github.com/wago-org/component-model/runtime` Contract at major version 1.
WASI and other component-world plugins consume that Contract, so the dependency
graph links and orders them without giving each consumer core-engine authority.
Contract calls are leased: shutdown rejects new operations and waits for an
in-flight component operation before stopping the provider.

There is no global registration, `component.Enable`, or compatibility entry
point that discovers an ambient plugin.

## Trust boundary

The component runtime is a linker and execution engine. It does not grant WASI
authority by itself. Filesystem, network, clock, random, and HTTP policy belongs
in a host package such as
[`github.com/wago-org/wasi`](https://github.com/wago-org/wasi). Hosts should
expose guest capabilities explicitly and keep them denied by default.

The plugin requests the separate `core.module.compile`,
`core.instance.instantiate`, and `core.funcref.create` Plugin Authorities. They
permit a trusted execution-model plugin to compile and instantiate embedded
core modules and own typed host-function references without exposing plugin
registration, policy, inspection, or arbitrary runtime lifecycle control.
Instance ownership remains bounded by its reviewed scope. Ordinary plugins use
narrower host-import, observation, interception, or managed-instance
Authorities. Wago revokes the handles during shutdown, and later component work
fails closed.

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
[`github.com/wago-org/wasi`](https://github.com/wago-org/wasi) host plugin.
