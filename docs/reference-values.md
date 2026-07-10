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

Token zero is reserved for null. Nullable `funcref` values execute through
function parameters/results, declared locals, direct calls, block results,
`ref.null`, and `ref.is_null` as one 64-bit slot. Non-null local `ref.func`
results now cross `Invoke`, typed `Call`, and the internal `invokeLocal` path as
stable random 64-bit tokens issued by a reference store. The token is mapped
back to the immutable internal descriptor only after an exact store lookup;
unknown, forged, cross-runtime, and cross-private-store tokens fail before native
entry. Descriptor, code, basedata, and linear-memory addresses never become the
public token.

Instances created by one `Runtime` share its reference store. Package-level
`Instantiate` creates a private store lazily on the first non-null funcref result,
so scalar-only standalone instances do not allocate a store and tokens from two
standalone instances are incompatible. Issuing a token retains the producer's
arena, code mapping, and home instance context. `Instance.Close` becomes a
logical close for such a producer and physical release is deferred until the
store releases its tokens: for a runtime store, after `Runtime.Close` and the
last attached instance closes; for a private store, when its instance closes.
Tokens are never reused within a store lifetime, so released-store tokens cannot
resolve through another store.

Local descriptors and same-runtime cross-instance function imports can now
produce public tokens. For an imported `ref.func`, the store first proves that
the returned descriptor is inside the returning instance's descriptor arena,
then reads its immutable `refSlot`, verifies the exact `InstanceExport` binding,
and resolves the canonical descriptor only against an instance registered in
the same store. The issued token is the producer's existing identity and retains
the producer, not the importer. A canonical local descriptor returned by
`table.get` follows the same registered-range path. Existing tokens remain
usable after producer logical close because their store entry is already an
exact retained root.

The store never dereferences public bits or an unvalidated `refSlot`. Corrupted
canonical metadata, cross-runtime/private-store imports, and host-import
funcrefs remain fail-closed and issue no token. Reference globals,
reflection-free host boundaries, and the store-owned, generation-checked
externref handle table are also still pending.

## Typed calls and signatures

Exported signature conversion preserves `funcref` and `externref` instead of
collapsing them to `i32`. Typed `Instance.Call` checks the exact reference type
and represents a reference in one full-width ABI slot. With reference types
enabled, nullable and store-owned local non-null `funcref` function signatures
are executable; incompatible tokens fail closed as described above, and
disabling the feature rejects those signatures explicitly. `externref`
signatures remain structural metadata only until the store-owned handle
implementation lands.

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

After the token store landed, the pinned single-CPU command
`taskset -c 0 go test ./src/wago -run '^$' -bench '^(BenchmarkInvokeAddOne|BenchmarkInvokeNullFuncref|BenchmarkInvokeNonNullFuncrefRoundTrip)$' -benchmem -count=5`
measured:

- scalar `BenchmarkInvokeAddOne`: 16.35–16.45 ns/op, median 16.38 ns/op,
  0 B/op, 0 allocs/op;
- null `BenchmarkInvokeNullFuncref`: four stable samples at 20.51–20.55 ns/op
  plus one 29.32 ns/op system outlier, always 0 B/op and 0 allocs/op; and
- same-runtime non-null token `id` round trip (one exact ingress lookup and one
  stable egress lookup): 35.10–37.15 ns/op, median 35.15 ns/op, 0 B/op,
  0 allocs/op.

After same-store imported canonicalization, the pinned single-CPU command
`taskset -c 0 go test ./src/wago -run '^$' -bench '^(BenchmarkCompileSmallScalar|BenchmarkInvokeAddOne|BenchmarkInvokeNullFuncref|BenchmarkInvokeLocalFuncrefEgress|BenchmarkInvokeImportedFuncrefEgress|BenchmarkInvokeNonNullFuncrefRoundTrip|BenchmarkRuntimeInstantiateSmallScalar)$' -benchmem -count=5`
measured stable medians (single scheduling outliers retained in the raw run):

- compile small scalar: 8.46 µs/op, 26,880 B/op, 62 allocs/op;
- scalar Invoke: 16.23 ns/op, 0 B/op, 0 allocs/op;
- null funcref Invoke: 20.59 ns/op, 0 B/op, 0 allocs/op;
- local non-null funcref egress: 28.42 ns/op, 0 B/op, 0 allocs/op;
- imported non-null funcref egress: 43.26 ns/op, 0 B/op, 0 allocs/op;
- same-runtime non-null token round trip: 35.39 ns/op, 0 B/op,
  0 allocs/op; and
- warmed Runtime instantiation of a small scalar module: 958.2 ns/op,
  1,224 B/op, 7 allocs/op.

A detached `61de8435` baseline using the same benchmark source measured scalar
Invoke at 16.60 ns/op, local funcref egress at 28.22 ns/op, non-null round trip
at 35.26 ns/op, and warmed Runtime instantiation at 965.4 ns/op with the same
1,224 B/op and 7 allocs/op. Imported egress initially exposed an unrelated
cross-instance-only host-log cost (8 B/op, 1 alloc/op); omitting the unused async
host log removed it. The registry therefore adds no steady-state scalar Invoke,
round-trip, compile, or warmed-instantiation allocation. Treat sub-nanosecond
single-host timing differences as noise/watchpoints rather than final gates.

On linux/amd64, `unsafe.Sizeof(Instance{})` remains 776 bytes versus 744 bytes at
`e54f9556`, a 32-byte per-instance increase for the store pointer and lifetime
state. `referenceStore` is now 48 bytes (+8 for the live-instance registry map
header) and is allocated once per `Runtime`; its registry is bounded by the
store's attached instances and reuses its map across warmed instantiations.
Standalone scalar/null-only instances keep a nil store, while first non-null
egress lazily creates the private store. Each issued token remains a 24-byte
entry plus two bounded-to-store-lifetime token indexes and intentionally retains
its producer resources until store teardown.

## Min-only funcref table growth

A local funcref table without a declared maximum now reserves a bounded 64-entry
capacity when its module executes `table.grow` or exports the table. This is an
implementation resource limit, not a synthetic Wasm declared maximum:
`table.grow` still returns `-1` without mutation beyond the reserved capacity.
A min-only table with no growth/export surface keeps capacity equal to its
minimum, preserving the fixed-use table-0 footprint. The Release 2
`table_grow.wast` growth from 10 to 20 now returns 10 and null-initializes every
new entry.

On July 9, 2026, pinned single-CPU runs used
`taskset -c 0 go test ./src/wago -run '^$' -bench '^(BenchmarkCompileSmallScalar|BenchmarkInvokeAddOne|BenchmarkInvokeTableGrowNull|BenchmarkRuntimeInstantiateSmallScalar|BenchmarkRuntimeInstantiateMinOnlyTableGrow)$' -benchmem -count=5`
and a paired fixed/growth table-instantiation run. Stable medians were 8.412
µs/op for scalar compile, 16.44 ns/op for scalar Invoke, 22.77 ns/op for
successful null `table.grow`, 948.8 ns/op for warmed scalar Runtime
instantiation, 1,001 ns/op for fixed-use min-only table instantiation, and 1,010
ns/op for growth-capable min-only table instantiation. The Invoke paths remain 0
B/op and 0 allocs/op; all three instantiation shapes remain 1,224 B/op and 7
allocs/op.

A detached `08476b11` baseline using the same benchmark source measured 8.461
µs/op, 17.59 ns/op, 940.5 ns/op, 998.8 ns/op, and 1,006 ns/op for scalar compile,
scalar Invoke, scalar instantiation, fixed-use table instantiation, and the table
module before it gained growth capacity. The 64-entry min=0 funcref reserve adds
2,048 off-heap descriptor bytes but no Go allocation; fixed-use min-only capacity
is unchanged. The timing deltas are within the observed run noise and do not
show an unjustified scalar or fixed-table regression.

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

With WABT 1.0.36 available on July 9, 2026, the Release 2 execution command now
reports 1,403 passed / 197 skipped modules and 46,234 passed / 2 failed / 1,978
skipped assertions. Both `table_grow.wast` failures are fixed; the two remaining
execution failures are the shared-memory/table assertions in `linking.wast`.
The remaining gaps are executable and visible, not hidden by a missing
converter; zero-skip conformance remains pending.
