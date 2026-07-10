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

Imported-table initialization is also a reference lifetime boundary. Active
segment writes are applied in declaration order, so writes from an earlier valid
segment remain visible if a later segment is out of bounds, a later active data
segment traps, or the start function traps. When such a failed instance leaves
one of its local funcrefs in a shared table, the table retains that instance's
arena, code mapping, and home context. Before adding a failed-instance root, the
table scans its finite descriptor slots and releases roots whose local
`refSlot` identities are no longer present. Retention is therefore bounded by
the shared table's capacity rather than by the number of failed
instantiations. Closing the table owner releases the remaining roots before its
descriptor arena is released. A focused capacity-one overwrite test performs
four failed instantiations, proves the prior failed producer is physically
released on every replacement, and proves table close releases the final root.

The shared-table root map is allocated only on the failed-instantiation path.
Local table export handles remain lazy, so ordinary local-table instantiation
keeps its existing Go allocation count. On July 9, 2026, pinned single-CPU runs
of scalar compile/Invoke plus scalar, fixed-table, and imported-table Runtime
instantiation measured medians of 8.660 us/op, 16.36 ns/op, 963.5 ns/op, 1,024
ns/op, and 1,304 ns/op. A detached `4d613c9b` run with the same benchmark source
measured 8.549 us/op, 16.29 ns/op, 942.7 ns/op, 990.9 ns/op, and 1,297 ns/op.
Allocation counts were unchanged: scalar Invoke stayed 0 B/op and 0 allocs/op;
scalar and fixed-table instantiation stayed 1,224 B/op and 7 allocs/op; imported-
table instantiation stayed 1,840 B/op and 9 allocs/op. The timing differences are
small relative to observed run noise and do not show an unjustified ordinary or
shared-table regression.

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
`e54f9556`. The shared-table lifetime object reuses the former 24-byte descriptor
footprint as an address/length plus a lazily allocated export handle, so ordinary
local-table instantiation adds no Go object and table-free instances do not grow.
`referenceStore` remains 48 bytes (+8 for the live-instance registry map
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

## Bulk segment drop state

WebAssembly 2.0 allows `memory.init`/`data.drop` and
`table.init`/`elem.drop` to name any in-range segment, regardless of its mode.
Active data and active element segments are applied during instantiation and
then become dropped; declarative element segments also start dropped. A
zero-length init with in-range zero source succeeds, a nonzero source range
traps, and repeated drops remain valid.

Wago keeps the original module index space in per-instance 16-byte descriptors.
Passive slots point at immutable compiled payloads and start at their full
length. Active/declarative slots have a zero pointer and zero length, so native
code never dereferences their payload bits. Descriptor arrays extend only
through passive or instruction-addressed indexes; ordinary active segments that
are never named by bulk instructions add no drop-state slot. This remains
bounded by the module's declared segment count and adds no process-global state.

The direct compiled path and `.wago` load path share the same zero-length state
proof. Focused tests verify active initialization effects, zero/nonzero init
bounds, repeated drops, original indexes, and active/declarative table state.
The scalar and fixed-table instantiation paths allocate no segment descriptors.
A paired pinned-CPU watchpoint against `3d50cab9` measured medians of 113.533 vs
114.706 us/op for the 256-function decode/validate fixture, 9.062 vs 8.819 us/op
for scalar compile, 16.84 vs 16.77 ns/op for scalar Invoke, 968.3 vs 986.1 ns/op
for warmed scalar Runtime instantiation, and 996.2 vs 1,028 ns/op for fixed
min-only table instantiation. Allocation counts were identical: decode/validate
51,358 B/op and 365 allocs/op; compile 26,880 B/op and 62 allocs/op; Invoke 0
B/op and 0 allocs/op; both instantiation shapes 1,224 B/op and 7 allocs/op. The
small timing shifts have no allocation or hot-path layout change and remain
within the run's observed noise.

## Imported function re-exports

An exported function may name an imported function. `Compiled.Signature` now
reports that import's structural parameter/result types, so raw `Invoke`, typed
`Call`, and the Release 2 harness do not mistake the export for an absent local
function. If the binding is an `InstanceExport`, invocation forwards to the
original producer's local function. Calling `ExportedFunc` on the relay returns
the same original handle rather than creating another owner, so chained imports
preserve traps, mutable producer state, code/context identity, and the existing
close-order rule: the producer remains open until every importer and re-exporter
that can execute it has closed.

A host-import re-export remains fail-closed as an `ExportedFunc`: a host binding
has no `InstanceExport` owner that can express its code/context lifetime. This is
separate from structural signature reporting and avoids silently broadening host
funcref egress.

Imported-export resolution reuses the existing four-slot Invoke cache by encoding
the import index in its existing local-index field; `Instance` therefore remains
776 bytes. Before caching, the forwarding benchmark repeatedly constructed the
"imported function" error on the fallback path and measured a 194.1 ns/op median,
80 B/op, and 3 allocs/op. The cached path measures 29.96 ns/op, 0 B/op, and 0
allocs/op. A paired detached-`f2f14eb8` watchpoint run versus the final tree
measured medians of 10.401 vs 9.289 us/op for scalar compile, 16.61 vs 16.60 ns/op
for scalar Invoke, 1,037 vs 1,037 ns/op for scalar Runtime instantiation, 1,098 vs
1,070 ns/op for fixed-table instantiation, and 1,382 vs 1,464 ns/op for imported-
table instantiation. Allocation counts stayed identical: scalar Invoke remained
0 B/op and 0 allocs/op; scalar/fixed instantiation remained 1,224 B/op and 7
allocs/op; imported-table instantiation remained 1,840 B/op and 9 allocs/op. The
imported-table timing shift has no corresponding instantiation or layout change
and remains a noise watchpoint rather than evidence of a regression.

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

## Declared `ref.func` validation

WebAssembly 2.0 permits a `ref.func` in a function body only when the target is
in the module validation context's declared-function set. Wago builds that set
before validating constant expressions or bodies. Function exports,
`ref.func` global/table initializer expressions, and both legacy-index and
expression element segments declare their referenced functions. Naming a
function as the start function does not declare it, and a function body does
not declare itself.

Both the AST and byte-backed validators enforce the same rule. The set is a
module-bounded bitset: modules with at most 64 functions use inline storage,
while larger modules allocate one bit per function. Declaration collection does
not use a process-global registry and malformed constant-expression bytes still
flow through the normal strict validation path.

The focused official Release 2 `ref_func.wast` proof passes all three valid
module sites and all three invalid assertions on both validators. The full
validation corpus still has unrelated pre-existing failures, so this closes the
`ref.func` declaration root rather than the complete WebAssembly 2.0 validation
gate. A paired pinned-CPU `BenchmarkDecodeValidate` run of the 256-function
fixture measured a median of 114.821 us/op at the pre-rule `f8d20081` baseline
and 113.725 us/op after the focused proof commits, with both runs at 51,358 B/op
and 365 allocs/op. The declaration check therefore adds no measured allocation
or compile-latency regression in that watchpoint.

## Release 2 execution harness

The official-suite harness encodes WABT's null `funcref` argument/result value as
one zero `uint64` slot and requires a zero result token. Null `externref` and
non-null reference values remain explicit reference-argument/reference-result
gaps; reference-valued globals remain reference-global gaps. This keeps gap
counts aligned with the subset the runtime actually executes.

With WABT 1.0.36 available on July 9, 2026, the Release 2 execution harness now
honors named modules, `register`, named actions, and `assert_uninstantiable` with
registered function, memory, table, and global imports. Imported function
re-exports also execute, reducing `linking.wast` from 14 absent-export skips to
zero. After closing the branch-table payload root, the current command reports
1,423 passed / 177 skipped modules and 46,384 passed / 0 failed / 1,864 skipped
assertions; remaining gaps are compile-rejected=97, instantiate-rejected=80,
module-unavailable=1,773, absent-export=0, reference-argument=36,
reference-result=55, and reference-global=0. The complete valid-module
validation gate is 1,600 passed / 0 failed / 0 skipped; invalid/malformed
assertions still have independent failures and skips. The `unreached-valid.wast`
line 49 module and its trap assertion now pass, along with the `linking.wast`
shared-memory/table start-trap assertions, earlier-segment persistence
assertions, imported function re-export assertions, and active/declarative
already-dropped operations. The remaining gaps are executable and visible, not
hidden by a missing converter; zero-skip conformance remains pending.
