# Public API migration

This release intentionally replaces the import and invocation APIs. There are
no deprecated aliases for the removed names.

| Before | Now |
|---|---|
| `wago.Imports{"env.f": value}` | `imports := wago.NewImports()` followed by the appropriate exact `(module, name)` registration |
| `imports.Module("env").Func("f", fn)` | `imports.HostFunc("env", "f", fn)` |
| plugin `HostImports().Module("env").Func("f", fn)` | plugin `HostImports().HostFunc("env", "f", fn)` |
| `HostFunc` / `CallerHostFunc` raw slot callbacks | `func(HostCall)` / `func(Caller, HostCall)` |
| signature-specific callback wrapper types | supported ordinary Go functions, or an explicit `HostCall` callback |
| `Instance.PrepareFunction` / `PreparedFunction` | `Instance.WasmFunc` / `WasmFunc` |
| `PrepareI32...` / `PreparedI32...` families | `WasmFunc(...).Invoke(...)` |
| `Invoke0` through `Invoke4` | `Invoke(args ...uint64)` |
| `PreparedSession`, `OpenSession`, and session invocation | resolve one `WasmFunc`; use `Invoke` normally or `OpenSession` for a caller-owned batch |
| `Instance.Call(ctx, export, values...)` | `Instance.InvokeValues(ctx, export, values...)` |

## Imports

`Imports` is now a structured declaration collection:

```go
imports := wago.NewImports()
imports.HostFunc("env", "add", func(a, b int32) int32 { return a + b })
imports.HostFunc("foo", "scale", func(call wago.HostCall) {
	call.SetF64(0, float64(call.I32(0))*call.F64(1))
}).Params(wago.ValI32, wago.ValF64).Results(wago.ValF64)
```

Module and field names remain separate identities, including when either name
contains a dot. `Function`, `Memory`, `Global`, `Table`, and `Tag` retain the
non-callback and cross-instance binding cases. `I32Event(module, name, fn)`
retains its explicit deferred delivery and limits.

The first instantiation seals the collection and takes an immutable binding
snapshot. A sealed collection may be reused concurrently for later
instantiations. Mutation is not concurrent-safe before sealing and is rejected
after sealing. Declaration errors are accumulated and fail finalization before
guest startup. Plugin declaration and authorization errors likewise fail before
activation even when `Plugin.Register` returns `nil`.

## Callbacks

Wago recognizes a finite reflection-free set of ordinary Go signatures. The
portable explicit forms are:

```go
func(call wago.HostCall)
func(caller wago.Caller, call wago.HostCall)
```

Use the caller-aware form for guest memory, reference operations, invocation
context, or authorized synchronous re-entry. `HostCall`, `Caller`,
`ParamSlots()`, `ResultSlots()`, and memory or reference views borrowed through
them must not be retained after the callback returns. V128 callbacks, non-null
exception-reference transfer, and unrestricted WasmGC reference transfer remain
unsupported.

Owned `HostFuncRef` values remain available. Their constructors accept the same
new callback forms and ordinary supported Go functions.

## Invocation

`WasmFunc` resolves and caches export entry/signature information without
executing guest code or retaining an instance reservation:

```go
step, err := instance.WasmFunc("step")
if err != nil {
	return err
}
out, err := step.Invoke(wago.I32(41))
```

`Invoke` accepts any arity supported by the module and validates the exact ABI
slot count. A V128 is one logical Wasm value but two public ABI slots. Returned
slot slices are borrowed instance storage and remain valid only until the next
invocation on that instance; copy results that must survive another call.

Use `Instance.Invoke` for by-name calls, `Instance.InvokeContext` for an explicit
per-call cancellation/deadline context, and `Instance.InvokeValues` for tagged
`Value` arguments and results. Resolved handles deliberately have no context
variant. For repeated calls on one instance, `WasmFunc.OpenSession` holds
invocation admission until `Close`; the session and instance must not be used
concurrently. Always close the session before closing the instance:

```go
session, err := step.OpenSession()
if err != nil {
	return err
}
defer session.Close()
out, err := session.Invoke(wago.I32(41))
```

An idle session reserves its instance and can block unrelated operations on
that instance. It does not retain a shared GC-domain lease between calls.

See [`examples/23-public-api`](../examples/23-public-api) for the complete,
executable migration target and reproducible guest fixture.
