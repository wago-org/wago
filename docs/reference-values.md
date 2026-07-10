# Public reference values

Wago's public value API represents WebAssembly 2.0 `funcref` and `externref`
with the following types:

- `ValFuncRef` and `ValExternRef` identify reference-valued signatures.
- `FuncRef` and `ExternRef` are opaque, comparable 8-byte token wrappers.
- `NullFuncRef()` and `NullExternRef()` construct null references; the zero value
  of either wrapper is also null.
- `ValueFuncRef`, `ValueExternRef`, `Value.FuncRef`, and `Value.ExternRef` move
  reference tokens through the typed `Value` API.

The underlying `uint64` slot is an opaque token. It is not a Go pointer, a JIT
code address, an instance-descriptor address, or an embedder object address.
Callers may pass a token back to wago, but must not interpret it or construct one
from a pointer. `Value.Bits` and `ValueOf` exist for the low-level slot API; for
reference values their bits have no meaning outside the runtime/store that
issued them.

Token zero is reserved for null. Nullable `funcref` values now execute through
function parameters/results, declared locals, direct calls, block results,
`ref.null`, and `ref.is_null` as one 64-bit slot. Non-null funcref token ownership
and the store-owned, generation-checked externref handle table are later phases
in `agent-todo.md`; globals and reflection-free host boundaries remain rejected.

## Typed calls and signatures

Exported signature conversion preserves `funcref` and `externref` instead of
collapsing them to `i32`. Typed `Instance.Call` checks the exact reference type
and carries a reference in one full-width ABI slot. With reference types enabled,
nullable `funcref` function signatures are executable; disabling the feature
rejects those signatures explicitly. `externref` signatures remain structural
metadata only until the store-owned handle implementation lands.

## `.wago` compatibility

Compiled-module codec version 18 adds the WebAssembly structural type codes
`0x70` (`funcref`) and `0x6f` (`externref`) for function signature metadata.
Version 17 blobs remain rejected by a version-18 loader, and older loaders reject
version-18 blobs, so reference metadata cannot be silently reinterpreted as a
numeric type.

Reference global metadata is rejected on both marshal and load. Even a null
reference global is not admitted through `.wago` until reference-global runtime
semantics and ownership are complete. In particular, live funcref/externref
tokens and externref store identity are never serialized. Element metadata may
continue to serialize function indexes and null initializers because those are
module structure, not live host references.

## Release 2 execution harness

The official-suite harness encodes WABT's null `funcref` argument/result value as
one zero `uint64` slot and requires a zero result token. Null `externref` and
non-null reference values remain explicit reference-argument/reference-result
gaps; reference-valued globals remain reference-global gaps. This keeps gap
counts aligned with the subset the runtime actually executes.
