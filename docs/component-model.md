# Component Model runtime

Wago's `src/component` package decodes and instantiates WebAssembly Components.
It supports the Preview 2 binary model, canonical ABI lift/lower, typed values,
resources, nested composition, and typed host imports. The package also contains
experimental Preview 3 task, future, and stream machinery.

## Trust boundary

The component runtime is a linker and execution engine. It does not grant WASI
authority by itself. Filesystem, network, clock, random, and HTTP policy belongs
in a host package such as [`wago-org/wasi`](https://github.com/wago-org/wasi).
Hosts should expose capabilities explicitly and keep them denied by default.

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

The component packages include decoder, canonical ABI, resource, composition,
async, oracle, and malformed-input tests. A real Rust `wasm32-wasip2`
`wasi:cli/command` fixture is exercised both in this repository and by the WASI
host module.
