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
Callers must not interpret reference bits or construct them from pointers.
`Value.Bits` and `ValueOf` remain available for the low-level slot API, but a
reference token is meaningful only under the runtime/store ownership policy that
issued it.

Token zero is reserved for null. Nullable `funcref` values now execute through
function parameters/results, declared locals, direct calls, block results,
`ref.null`, and `ref.is_null` as one 64-bit slot. Until runtime-owned non-null
funcref tokens and lifetimes exist, every public call boundary is deliberately
zero-only: `Invoke`, typed `Call`, and the internal `invokeLocal` path used by
re-exports reject a nonzero funcref argument before native entry. A nonzero
funcref result is cleared from the reusable public result buffer and rejected
without returning its bits. Internal Wasm-to-Wasm non-null funcrefs continue to
use instance descriptors, but those addresses are never a public token.

Non-null funcref token ownership and the store-owned, generation-checked
externref handle table are later phases in `agent-todo.md`; reference globals and
reflection-free host boundaries remain rejected.

## Typed calls and signatures

Exported signature conversion preserves `funcref` and `externref` instead of
collapsing them to `i32`. Typed `Instance.Call` checks the exact reference type
and represents a reference in one full-width ABI slot. With reference types
enabled, nullable `funcref` function signatures are executable; public non-null
funcrefs fail closed as described above, and disabling the feature rejects those
signatures explicitly. `externref` signatures remain structural metadata only
until the store-owned handle implementation lands.

## Boundary guard performance

The public guard caches two booleans with each resolved export signature, so a
cached scalar-only `Invoke` performs no per-call type walk and allocates nothing.
On July 9, 2026, the pinned single-CPU command
`taskset -c 0 go test ./src/wago -run '^$' -bench '^(BenchmarkInvokeAddOne|BenchmarkInvokeNullFuncref)$' -benchmem -count=5`
measured:

- before the guard (`aa0b6375`), scalar `BenchmarkInvokeAddOne`: 15.65–15.89
  ns/op, median 15.78 ns/op, 0 B/op, 0 allocs/op;
- with the guard, scalar `BenchmarkInvokeAddOne`: 16.09–16.31 ns/op, median
  16.13 ns/op, 0 B/op, 0 allocs/op; and
- null-funcref `BenchmarkInvokeNullFuncref`: four stable samples at 20.43–21.18
  ns/op plus one 29.80 ns/op system outlier, always 0 B/op and 0 allocs/op.

The measured scalar median increase is about 2.2%, the cost of the predictable
fail-closed safety checks; it adds no allocation or signature walk to the cached
scalar path. Re-measure when runtime-owned non-null token translation replaces
this temporary zero-only policy.

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
