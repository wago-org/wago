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
the same store. When the producer already has a descriptor arena, the issued
token is the producer's existing identity and retains the producer directly. A
bare producer that never executes reference operations has no such arena; in
that case the importer's exact proxy descriptor becomes the store identity, and
token retention keeps the importer physically live while its function attachment
retains the producer. The store also keys tokens by `(producer, local function)`
so separate importers still receive one stable identity. A canonical local
descriptor returned by `table.get` follows the same registered-range path.
Existing tokens remain usable after logical close because their complete
retention chain remains rooted until store teardown.

The store never dereferences public bits or an unvalidated `refSlot`. Corrupted
canonical metadata, cross-runtime/private-store imports, and unowned host-import
funcrefs remain fail-closed and issue no token. Local and imported/shared
`funcref` globals use the same exact token/descriptor translation described below,
including structural `ref.func` initializers and imported immutable `global.get`
copies. Explicit host descriptor ownership is described next; reflection-free
externref host boundaries follow it.

## Explicit host funcref ownership

`Runtime.NewHostFuncRef(fn, sig)` creates a 112-byte Go ownership handle for one
reflection-free `HostFunc`, exact Wasm signature, and exact Runtime reference
store. The owner occupies one bounded 24-byte slot in the store's existing
externref/dispatch slice, so `referenceStore` remains 88 bytes and there is no
process-global registry. The handle is supplied directly as an `Imports` value.
Raw `HostFunc` values remain callable, but `ref.func`/table egress of their
per-instance descriptors is rejected because no explicit owner can retain them.

Each importing instance still creates its own native thunk and immutable 32-byte
descriptor. On first public egress, the store validates the descriptor range,
import index, exact signature id, nonzero thunk address, home linear-memory base,
and self `refSlot`; it then canonicalizes the `HostFuncRef` to that descriptor,
retains the source instance's arena/code/thunk/context, and issues one stable
opaque token. A second instance importing the same owner resolves to the same
token. Cross-runtime owners, forged tokens, stale stores, and corrupted metadata
fail before native entry or token issue.

Owned indirect thunks use the active caller's synchronous control frame and a
store-bounded dispatch index. This lets a token enter another compatible
instance's funcref table and call the exact host function while `HostModule`
correctly names the active caller. Only a module that both has a funcref table and
accepts a public funcref parameter installs the 328-byte off-heap control frame;
ordinary scalar and fixed local table-0 modules do not enter sync mode or read
heterogeneous metadata. The footprint validator accounts that control frame
exactly rather than relying on arena slack.

Host callbacks with funcref parameters receive opaque public tokens, never native
descriptor addresses. Funcref results are resolved back through the same store
before native re-entry; forged and cross-runtime callback results fail while the
post-call Wasm continuation remains unexecuted. Host-created non-null references
originate only from an explicit `HostFuncRef` descriptor; no API infers function
identity or ownership from a raw Go function value or address.

`HostFuncRef.Close` rejects live importers. Once a token exists it also rejects
close while Runtime instances can still consume that token. Close every importer,
close the Runtime, then close the owner; the final store-object close releases the
token root and only then unmaps the retained thunk/source resources. Host-created
funcref globals reuse that issued-token proof as described below. Host-import
re-export through `ExportedFunc` remains fail-closed because that API specifically
returns an `InstanceExport` owner.

Pinned single-CPU three-second medians on July 10, 2026 are 38.83 ns/op for stable
owned descriptor egress and 121.0 ns/op for a same-store indirect call through the
public token, both 0 B/op and 0 allocs/op. Warmed funcref-ingress caller
instantiation is 1,283 ns/op, 1,296 B/op, and 10 allocs/op; warmed owned-host-
funcref instantiation is 9,974 ns/op, 2,528 B/op, and 22 allocs/op because it maps
the explicit per-instance thunk. In the same run, DecodeValidate measured
118.184 us/op, scalar compile 9.525 us/op, scalar Invoke 16.26 ns/op, fixed
table-0 indirect 18.30 ns/op, scalar instantiation 1,063 ns/op, and fixed-table
instantiation 1,191 ns/op. Ordinary allocation counts remain 365 and 62 allocs/op
for validation/compile, 0/0 for Invoke paths, and 1,224 B/op plus 7 allocs/op for
scalar/fixed-table instantiation. `Global`, `Table`, `Compiled`, `Instance`,
`tableDef`, and `referenceStore` remain 40, 64, 632, 776, 40, and 88 bytes.

The measurement commands are:

```sh
taskset -c 0 go test ./src/core/compiler/wasm -run '^$' \
  -bench '^BenchmarkDecodeValidate$' -benchmem -benchtime=3s -count=5

taskset -c 0 go test ./src/wago -run '^$' \
  -bench '^(BenchmarkCompileSmallScalar|BenchmarkInvokeAddOne|BenchmarkInvokeTable0IndirectFixed|BenchmarkInvokeOwnedHostFuncrefEgress|BenchmarkInvokeOwnedHostFuncrefIndirect|BenchmarkRuntimeInstantiateSmallScalar|BenchmarkRuntimeInstantiateMinOnlyTableFixed|BenchmarkRuntimeInstantiateFuncrefIngressCaller|BenchmarkRuntimeInstantiateOwnedHostFuncref)$' \
  -benchmem -benchtime=3s -count=5
```

## Host-created funcref globals

`Runtime.NewFuncRefGlobal(initial, mutable)` creates a host-owned 8-byte funcref
cell. `initial` is either null or an opaque `FuncRef` token already issued by the
same Runtime store. A host function therefore becomes eligible only after an
explicit `HostFuncRef` import has produced its validated token; the constructor
does not accept a `HostFunc`, infer identity from a Go function pointer, or create
a second descriptor registry.

For a non-null initializer, the constructor resolves the token through the
existing store and writes only its internal descriptor into the mmap-backed cell.
The `globalOwner` has the exact store but no synthetic producer instance. Typed
`Global.GetValue` reuses the already-retained descriptor-to-token entry, and
`SetValue` accepts only null or another token from that same store. A missing,
forged, cross-runtime, stale, or raw-unowned descriptor fails before storage or
public egress; arbitrary token bits are never dereferenced.

The host global counts as one bounded store object, so its token entry continues
to retain the true producer's arena, thunk, code mapping, and home context after
the producer instance logically closes. Imports share the exact `*Global` cell;
duplicate aliases attach one root per instance, mutation is immediately visible,
and the token remains callable through another same-runtime table. `Global.Close`
rejects live importers. The explicit `HostFuncRef` owner continues to reject close
while Runtime instances can consume its token; after instances and the Runtime
close, the owner may close while the still-live global keeps the token entry until
its own final close.

Codec version 21 serializes only the global's structural type, mutability, import/
export identity, and null/`ref.func`/imported-`global.get` initializer. Snapshots
continue to reject reference globals. The runtime `Global`, its token,
`HostFuncRef` owner, dispatch index, descriptor, thunk address, producer, and
store identity are never serialized.

Pinned single-CPU three-second medians on July 10, 2026 are 9.245 ns/op for
cached non-null `Global.GetValue`, with 0 B/op and 0 allocs/op, and 1,660 ns/op,
1,960 B/op, and 14 allocs/op for warmed imported funcref-global instantiation.
The externref-global and numeric-global controls in the same run measured 1,589
and 1,564 ns/op with 1,960/1,968 B/op and 14 allocs/op. Null funcref global JIT
round trips measured 21.52 ns/op at 0 B/op and 0 allocs/op.

The same run measured DecodeValidate at 122.307 us/op, scalar compile at 11.449
us/op, scalar Invoke at 16.83 ns/op, fixed table-0 indirect dispatch at 19.59
ns/op, warmed scalar instantiation at 1,076 ns/op, and fixed-table instantiation
at 1,098 ns/op. Allocation watchpoints remain exact: 51,354 B/op and 365
allocs/op for DecodeValidate, 26,880 B/op and 62 allocs/op for compile, 0/0 for
Invoke paths, and 1,224 B/op plus 7 allocs/op for scalar/fixed-table
instantiation. `Global`, `HostFuncRef`, `Compiled`, `Instance`, and
`referenceStore` remain 40, 112, 632, 776, and 88 bytes. Timing moved broadly on untouched paths versus the preceding documented
run, while allocation and layout watchpoints remained exact, so the movement is
retained as scheduler/frequency noise rather than attributed to this global path.

The measurement commands are:

```sh
taskset -c 0 go test ./src/core/compiler/wasm -run '^$' \
  -bench '^BenchmarkDecodeValidate$' -benchmem -benchtime=3s -count=5

taskset -c 0 go test ./src/wago -run '^$' \
  -bench '^(BenchmarkCompileSmallScalar|BenchmarkInvokeAddOne|BenchmarkInvokeTable0IndirectFixed|BenchmarkInvokeNullFuncrefGlobalRoundTrip|BenchmarkRuntimeInstantiateSmallScalar|BenchmarkRuntimeInstantiateMinOnlyTableFixed|BenchmarkRuntimeInstantiateImportedFuncRefGlobal|BenchmarkStoreBoundFuncRefGlobalGetValue|BenchmarkRuntimeInstantiateImportedExternrefGlobal|BenchmarkRuntimeInstantiateImportedNumericGlobal)$' \
  -benchmem -benchtime=3s -count=5
```

## Shared standard memory and imported-memory re-export

`NewSharedMemory(min, max)` creates a host-owned memory that several compatible
instances may import at once. It preserves one byte/grow state, counts live
importers, and rejects `Close` until every importer has physically released its
attachment. The public `Memory` handle remains 16 bytes (two pointers). A
24-byte lifecycle sidecar is allocated only for host-created or exported/shared
memory; ordinary instance-owned scalar memories leave that pointer nil, preserving
1,224 B/op and 7 allocs/op for warmed scalar instantiation.

Exporting a locally owned memory lazily binds that sidecar to the producer.
Exporting an imported memory returns the exact original `*Memory`; it does not
copy bytes or create a relay owner. Every direct or re-export consumer retains
the original producer's job memory/basedata until its final close. Closing the
producer first is therefore a logical close, but new importers are rejected and
existing consumers must close before physical release. No process-global
registry or Go pointer is placed in linear memory.

The Release 2 harness creates one file-scoped shared memory with limits 1/2,
alongside the existing file-scoped table, and binds seven exact reflection-free
no-op functions: `print`, `print_i32`, `print_i64`, `print_f32`, `print_f64`,
`print_i32_f32`, and `print_f64_f64`. Files never share memory state with one
another. Live instances close before the standard memory and table.

Pinned single-CPU three-second medians on July 10, 2026 are 815.0 ns/op,
1,832 B/op, and 9 allocs/op for shared-memory import instantiation and 798.4
ns/op, 1,824 B/op, and 8 allocs/op for imported-memory re-export instantiation.
The final run measured scalar compile at 14.562 us/op, scalar Invoke at 25.28
ns/op, fixed table-0 indirect at 21.53 ns/op, scalar instantiation at 1,292
ns/op, and fixed-table instantiation at 1,083 ns/op. A preceding isolated
DecodeValidate run measured 120.595 us/op. Ordinary allocations remain 365 and
62 allocs/op for validation/compile, 0/0 for Invoke paths, and 1,224 B/op plus 7
allocs/op for scalar/fixed-table instantiation. Broad timing movement affected
untouched compile/Invoke/instantiation paths while allocation and layout
watchpoints remained exact, so it is retained as scheduler/frequency noise rather
than attributed to the memory-owner sidecar.

The measurement command is:

```sh
taskset -c 0 go test ./src/wago -run '^$' \
  -bench '^(BenchmarkCompileSmallScalar|BenchmarkInvokeAddOne|BenchmarkInvokeTable0IndirectFixed|BenchmarkRuntimeInstantiateSmallScalar|BenchmarkRuntimeInstantiateMinOnlyTableFixed|BenchmarkRuntimeInstantiateSharedMemoryImport|BenchmarkRuntimeInstantiateImportedMemoryReexport)$' \
  -benchmem -benchtime=3s -count=5
```

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

## Externref handles and host boundaries

Executable `externref` signatures, params/results, zero-initialized locals,
blocks, branches, typed `select`, `ref.null extern`, and `ref.is_null` use one
64-bit handle slot. Handle zero remains null. Non-null handles index a Go-owned,
per-`referenceStore` slot table with an exact generation check; the public token
is store-keyed before it crosses the API, so a token from another runtime or
standalone private store does not decode to that store's slot/generation pair.
Native code only copies or tests the uint64 handle. Go interface values never
enter mmap-backed locals, stacks, globals, or tables.

`Runtime.NewExternRef` and `Instance.NewExternRef` register embedder objects.
`Runtime.ExternRefValue` and `Instance.ExternRefValue` resolve only compatible
store tokens. Runtime-created instances share one store; standalone instances
create a lazy private store on their first non-null reference registration.
Cross-instance functions with externref params/results link only when producer
and consumer use that same store; cross-runtime and standalone/private-store
bindings are rejected at instantiation. The `HostModule` value passed to
reflection-free `HostFunc` callbacks also implements `ExternRefHostModule`,
which exposes the same two operations without enlarging the base interface. Host
externref arguments are checked before callback entry, and host results are
checked before Wasm re-entry, so forged, stale, and incompatible results cannot
resume native execution.

Externref roots live for the reference-store lifetime. `Runtime.Close` releases
them immediately when no instance remains attached, or after the last attached
instance closes; closing a standalone instance releases its private slots.
Generation mismatch, released-store, forged-token, cross-runtime, and
cross-private-store tests all fail before native execution. The store is finite
for a process lifetime because every slot is owned by one runtime/private store
and no process-global registry exists. This first slice intentionally does not
add reclamation before store teardown.

Module-local externref globals and tables use 8-byte persistent handle cells.
Runtime-owned and locally exported externref globals/tables carry explicit
compatible-store owners and close-order contracts; same-runtime imported aliases
share exact identity, while cross-runtime/private-store imports reject.

On July 10, 2026, pinned three-second medians were 21.52 ns/op for null externref
Invoke, 33.54 ns/op for a non-null same-store identity round trip, and 132.4
ns/op for a synchronous host externref round trip; all remain 0 B/op and 0
allocs/op. The scalar synchronous host-call median in the same run was 108.3
ns/op. Warmed Runtime instantiation of the externref-control fixture measured
1,018 ns/op, 1,224 B/op, and 7 allocs/op versus 1,013 ns/op, 1,224 B/op, and 7
allocs/op for the scalar fixture. Each registered object occupies one 24-byte
Go slot plus amortized slice backing. `referenceStore` grows from 48 to 88 bytes
(+40 once per Runtime/private store), while `Instance` remains 776 bytes.

The same pinned run measured DecodeValidate at 120.004 us/op, scalar compile at
10.872 us/op, scalar Invoke at 16.61 ns/op, fixed table-0 indirect dispatch at
19.29 ns/op, and warmed scalar instantiation at 1,013 ns/op. The documented
`16a78af5` medians were 128.205 us/op, 12.826 us/op, 18.49 ns/op, 20.65 ns/op,
and 1,231 ns/op respectively. Allocation counts remain unchanged at 51,354
B/op and 365 allocs/op, 26,880 B/op and 62 allocs/op, 0/0 for Invoke paths, and
1,224 B/op plus 7 allocs/op for scalar instantiation. Timing moved broadly on
untouched paths, so the differences remain scheduler/frequency watchpoints, not
attributed gains.

## Local externref globals

Module-local immutable and mutable `externref` globals use one 8-byte native
handle cell. `ref.null extern` initializes zero. JIT `global.get` and `global.set`
copy the handle without translation; a non-null value enters through `Invoke`,
typed `Call`, or `SetGlobalValue` only after exact validation against the
instance's reference store. `GlobalValue` validates the stored handle again before
returning the unchanged opaque token. Forged, stale, cross-runtime, and
cross-private-store tokens fail before storage or public egress.

Raw `Instance.Global`/`SetGlobal` and `Global.Get`/`Set` remain fail-closed for
reference types, so token bits cannot bypass typed validation. Runtime-created
instances already have their shared store registered before start execution;
standalone instances can lazily create their private store from a host callback or
public registration. Closing the runtime releases roots after its last attached
instance closes, and closing a standalone instance releases its private roots.

Imported/shared externref globals use an explicit store-bound owner and
close-order contract. `Runtime.NewExternRefGlobal` creates a host-owned 8-byte
cell initialized from null or a token issued by that exact Runtime. Local exported
reference globals lazily bind the producer instance and its store; same-runtime
consumers may import or re-export the exact object. Cross-runtime and standalone
private-store imports reject before instantiation, as do type, mutability, forged
handle, corrupted descriptor, and public-owner-metadata mismatches.

Duplicate import aliases validate every declaration but attach one lifetime root.
Host global close rejects live importers. A local producer remains physically live
after logical close until every consumer detaches, preserving funcref code/context
and externref store identity. A Runtime-owned externref global counts as a bounded
store owner, so `Runtime.Close` retains roots until the final instance and global
close. The reference store reuses its existing live-object counter for tables and
globals, keeping `referenceStore` at 88 bytes; `Global` replaces its arena pointer
with a same-size owner pointer and remains 40 bytes.

Imported immutable `global.get` initializers copy the validated reference into a
local 8-byte cell. Funcref copies preserve the canonical descriptor and true
producer; externref copies preserve the generation-checked handle. No Go pointer
enters mmap-backed storage. `.wago` codec version 21 persists the structural reference-global declaration and
initializer, while snapshots continue to reject reference-global state. No live
handle, descriptor, producer identity, or store identity is serialized.

On July 10, 2026, pinned three-second medians were 24.28 ns/op for a null and
33.45 ns/op for a non-null externref global set/get Invoke round trip, both 0 B/op
and 0 allocs/op. Warmed Runtime instantiation of the two-global fixture measured
1,104 ns/op, 1,320 B/op, and 9 allocs/op. Each cell is exactly 8 off-heap bytes;
`Instance` remains 776 bytes and `referenceStore` remains 88 bytes.
DecodeValidate, scalar compile, scalar Invoke, fixed table-0 indirect, and warmed
scalar instantiation measured 120.486 us/op, 10.624 us/op, 17.68 ns/op, 18.85
ns/op, and 1,031 ns/op with unchanged allocation counts. The timing spread versus
the preceding documented run affects untouched paths in both directions and
remains scheduler/frequency noise rather than an attributed regression.

The shared-global measurement commands are:

```sh
taskset -c 0 go test ./src/wago -run '^$' \
  -bench '^(BenchmarkCompileSmallScalar|BenchmarkInvokeAddOne|BenchmarkInvokeTable0IndirectFixed|BenchmarkRuntimeInstantiateSmallScalar|BenchmarkRuntimeInstantiateExternrefTable|BenchmarkRuntimeInstantiateImportedTable|BenchmarkRuntimeInstantiateImportedExternrefTable|BenchmarkRuntimeInstantiateImportedExternrefGlobal|BenchmarkRuntimeInstantiateImportedNumericGlobal|BenchmarkStoreBoundExternrefGlobalGetValue)$' \
  -benchmem -benchtime=3s -count=5

taskset -c 0 go test ./src/wago -run '^$' \
  -bench '^(BenchmarkInvokeNumericGlobalRoundTrip|BenchmarkInvokeNullFuncrefGlobalRoundTrip|BenchmarkInvokeNullExternrefGlobalRoundTrip|BenchmarkInvokeNonNullExternrefGlobalRoundTrip)$' \
  -benchmem -benchtime=3s -count=5
```

For the shared-global slice, pinned three-second medians were 2,010 ns/op,
1,960 B/op, and 14 allocs/op for warmed imported externref-global instantiation
versus 1,938 ns/op, 1,968 B/op, and 14 allocs/op for the imported numeric-global
control. Cached host `GetValue` on a non-null runtime-owned externref global was
8.464 ns/op at 0 B/op and 0 allocs/op. Numeric, null funcref, null externref, and
non-null externref global set/get Invoke round trips measured 16.90, 23.52, 21.30,
and 34.08 ns/op respectively, all allocation-free.

The same run measured DecodeValidate at 298.261 us/op, scalar compile at 27.844
us/op, scalar Invoke at 22.32 ns/op, fixed table-0 indirect dispatch at 22.09
ns/op, warmed scalar instantiation at 1,202 ns/op, local externref-table
instantiation at 1,025 ns/op, imported funcref-table instantiation at 1,381 ns/op,
and imported externref-table instantiation at 1,406 ns/op. Allocation counts are
unchanged at 51,356-51,358 B/op and 365 allocs/op, 26,880 B/op and 62 allocs/op,
0/0 for Invoke paths, 1,224 B/op and 7 allocs/op for scalar/local-table
instantiation, and 1,840 B/op and 9 allocs/op for imported-table instantiation.
`Global`, `Compiled`, `Instance`, and `referenceStore` remain 40, 632, 776, and 88
bytes. Broad timing movement affects untouched paths and is retained as
scheduler/frequency noise, not an attributed regression.

## Externref tables

Module-local externref tables store one opaque 8-byte handle per entry behind the
same `[len u32][capacity u32]` header used by funcref tables. Native code only
copies, fills, grows, or tests those handle bits; it never resolves them or
places a Go pointer in mmap-backed storage. Public and host-call ingress validates
the exact instance store before native entry, and result egress validates the
stored handle again before returning it. Null initialization is all-zero storage.
Forged, stale, cross-runtime, and cross-private-store tokens therefore fail before
storage or public egress.

The compiled table index carries an exact element type. Table 0 keeps its direct
basedata descriptor load, and nonzero tables keep the compact pointer directory;
the generated instruction selects an 8-byte or 32-byte stride at compile time.
Scalar and funcref table-0 code does not read heterogeneous runtime metadata.
Externref-only table modules allocate no canonical funcref descriptor arena. A
fixed capacity-four externref table has an exact 40-byte descriptor (8-byte header
plus four entries). A min-only externref table that can grow reserves a bounded
1,024-entry window, or 8,192 entry bytes, so the official growth-to-803 case is
executable without an unbounded remap or Go allocation. Growth beyond the reserve
returns `-1` without mutation.

Local, imported, exported, and re-exported externref tables support
`table.get`, `table.set`, `table.size`, `table.grow`, `table.fill`, `table.copy`,
`table.init`, and `elem.drop` at table 0 and nonzero indexes in heterogeneous
modules, including active/passive/declarative null element segments.
`Runtime.NewExternRefTable` creates
host-owned typed 8-byte storage bound to that runtime's exact reference store.
A local externref export carries the producer's store identity; same-runtime
consumers may import or re-export the exact handle, while cross-runtime and
standalone/private-store imports fail before instantiation. Funcref and externref
table handles also reject element-type mismatches before descriptor access.

The 64-byte public `Table` replaces its former arena pointer with a same-size
pointer to a small owner object containing the arena or producer instance, exact
element type, compatible store, and importer count. Host table close rejects live
importers. Local table imports retain the producer's physical resources until the
consumer detaches, even if the producer is logically closed first. A runtime-owned
externref table counts as a store owner, so `Runtime.Close` releases roots only
after the last attached instance and table close; repeated close is idempotent.
Importer tracking uses four inline pointers before allocating overflow state, so
the common one- and two-table paths add no Go allocation.

Typed element metadata records the exact reference type, destination table,
active/passive/declarative mode, and structural null/ref.func payload. Null is an
explicit bit rather than the former `uint32` sentinel, so it cannot alias an
ordinary function index. Active externref segments write all-zero 8-byte handles;
passive payloads allocate exactly 8 bytes per entry plus one 16-byte per-instance
drop descriptor. Declarative and active state slots start dropped. No Go pointer
or live token is serialized in compiled metadata.

`table.init` and `table.copy` select 8-byte externref or 32-byte funcref strides at
compile time after exact validator type checks. Copies use memmove overlap
semantics and native code never resolves externref handles. Active bounds checks
remain all-or-nothing per segment, while earlier valid segments retain their
specified declaration-order effects if a later segment fails. Failed externref
initialization adds no producer root because handles belong to the store; failed
funcref retention remains unchanged and capacity-bounded. `elem.drop` now also
works in modules with no table by installing only the bounded descriptor state it
needs.

Codec version 21 preserves every runtime-relevant table index/type/import/export/
limit and typed element mode/destination/null-or-`ref.func` payload. It records no
table cell contents, token, descriptor, owner, or store identity. Snapshots
continue to reject every table module. Inert unexported tables
whose huge declared spare capacity cannot fit the bounded arena are represented at
their minimum only when no grow/export surface can observe that spare capacity;
ordinary table capacities and all observable grow/export limits are unchanged.

With WABT 1.0.36, the full Release 2 execution gate is now green at 1,600
modules and 48,248 assertions with zero failures, skips, or gap reasons.
`bulk.wast`, `elem.wast`, and `table.wast` remain fully executable at 13/104,
29/37, and 9/0 modules/assertions; `table_get.wast`, `table_copy.wast`,
`table_init.wast`, and `ref_func.wast` remain green at 1/10, 52/1,675, 35/677,
and 3/10. The former standard-import and imported-memory re-export gaps are
closed by the file-scoped host module described below.

The pinned measurement command is:

```sh
taskset -c 0 go test ./src/wago -run '^$' \
  -bench '^(BenchmarkCompileSmallScalar|BenchmarkInvokeAddOne|BenchmarkInvokeTable0IndirectFixed|BenchmarkRuntimeInstantiateSmallScalar|BenchmarkRuntimeInstantiateExternrefTable|BenchmarkRuntimeInstantiateImportedTable|BenchmarkRuntimeInstantiateImportedExternrefTable)$' \
  -benchmem -benchtime=3s -count=5

taskset -c 0 go test ./src/wago -run '^$' \
  -bench '^BenchmarkExportedExternrefTableCached$' \
  -benchmem -benchtime=3s -count=5
```

On July 10, 2026, pinned three-second medians for the shared-table slice were
1,379 ns/op, 1,840 B/op, and 9 allocs/op for warmed imported externref-table
instantiation versus 1,416 ns/op with the same allocation counts for the imported
funcref-table control. Cached local externref export lookup measured 25.19 ns/op,
0 B/op, and 0 allocs/op. Local fixed capacity-one externref-table instantiation
measured 1,021 ns/op, 1,224 B/op, and 7 allocs/op.

DecodeValidate, scalar compile, scalar Invoke, fixed funcref table-0 indirect
dispatch, and scalar instantiation medians were 118.701 us/op, 11.409 us/op,
16.28 ns/op, 18.51 ns/op, and 984.7 ns/op with allocation counts unchanged at
51,354 B/op and 365 allocs/op, 26,880 B/op and 62 allocs/op, 0/0 for Invoke, and
1,224 B/op plus 7 allocs/op for scalar instantiation. `Compiled`, `Instance`,
`Table`, `tableDef`, and `referenceStore` remain 632, 776, 64, 40, and 88 bytes.
Timing movement versus the preceding documented run affects untouched paths in
both directions and is retained as scheduler/frequency noise rather than an
attributed regression. The earlier local set/get measurements remain 21.52/33.52
ns/op at 0 B/op and 0 allocs/op.

The typed-element measurement commands are:

```sh
taskset -c 0 go test ./src/core/compiler/wasm -run '^$' \
  -bench '^BenchmarkDecodeValidate$' -benchmem -benchtime=3s -count=5

taskset -c 0 go test ./src/wago -run '^$' \
  -bench '^(BenchmarkCompileSmallScalar|BenchmarkInvokeAddOne|BenchmarkInvokeTable0IndirectFixed|BenchmarkRuntimeInstantiateSmallScalar|BenchmarkRuntimeInstantiateExternrefTable|BenchmarkRuntimeInstantiatePassiveExternrefElements|BenchmarkInvokeTable0CopyFuncref|BenchmarkInvokeTable0InitFuncref|BenchmarkInvokeTable0CopyExternref|BenchmarkInvokeTable0InitExternref)$' \
  -benchmem -benchtime=3s -count=5
```

Pinned three-second medians are 91.99 ns/op for four-entry funcref table-0 copy,
26.25 ns/op for funcref table-0 init, 69.78 ns/op for externref copy, and 19.58
ns/op for externref init, all at 0 B/op and 0 allocs/op. Warmed instantiation of
a fixed externref table is 1,090 ns/op, 1,224 B/op, and 7 allocs/op; adding one
passive four-null externref segment measures 1,168 ns/op, 1,216 B/op, and 6
allocs/op and adds exactly 48 arena bytes (16 descriptor plus four 8-byte
entries).

The same run measured DecodeValidate at 117.768 us/op and 51,354 B/op / 365
allocs/op, scalar compile at 11.444 us/op and 26,880 B/op / 62 allocs/op, scalar
Invoke at 16.92 ns/op, fixed funcref table-0 indirect at 18.46 ns/op, and warmed
scalar instantiation at 1,061 ns/op, 1,224 B/op, and 7 allocs/op. Invoke paths
remain allocation-free. `Global`, `Table`, `Compiled`, `Instance`, `tableDef`, and
`referenceStore` remain 40, 64, 632, 776, 40, and 88 bytes. Timing movement
versus prior documented runs affects untouched paths in both directions and is
retained as scheduler/frequency noise; allocation and layout watchpoints show no
unjustified scalar, table-0, compile, instantiation, or footprint regression.

## Local funcref globals

Module-local immutable and mutable `funcref` globals use an 8-byte native cell.
A `ref.null func` initializer stores zero. A structural `ref.func` initializer
stores a Wasm function index in compiled metadata and resolves it to the
instance's canonical descriptor only after code mapping; neither serialized nor
public metadata contains the descriptor address. JIT `global.get` and
`global.set` copy the internal 64-bit descriptor representation directly, so a
non-null token accepted at `Invoke`, typed `Call`, or `SetGlobalValue` is resolved
through the instance's exact reference store before it reaches the cell.
`GlobalValue` performs the inverse checked translation and returns the stable
token already owned by that store. The token entry retains the true producer's
arena, code, and home context, so a global can continue returning the value after
the producer's logical close.

The raw numeric `Instance.Global`/`SetGlobal` methods reject reference globals.
An exported `*Global` returns zero from `Get` and rejects `Set` for a reference
type, preventing either an internal descriptor address or unvalidated public
token bits from crossing that lower-level API. Null typed access remains
allocation-free; non-null access reuses the existing store token entry.

Imported/shared funcref and externref globals use the exact compatible-store
owner and close-order model described above. A `ref.func` of an imported
`InstanceExport` remains internally callable and keeps exact `refSlot`
canonicalization against the true producer descriptor arena. Explicitly owned
host-import descriptors and host-created funcref globals use the HostFuncRef/token
model above. Raw unowned host descriptors still fail closed.

On July 10, 2026, the pinned single-CPU null global set/get benchmark measured a
21.49 ns/op median with 0 B/op and 0 allocs/op. Warmed Runtime instantiation of
the two-global fixture measured 1,044 ns/op, 1,320 B/op, and 9 allocs/op; warmed
scalar instantiation remained 1,224 B/op and 7 allocs/op. Against red baseline
`713bb939`, DecodeValidate, scalar compile, scalar Invoke, and warmed scalar
instantiation medians were 116.247 vs 117.876 us/op, 9.466 vs 9.764 us/op, 16.44
vs 16.48 ns/op, and 967.7 vs 1,005 ns/op, with allocation counts unchanged.

The structural `ref.func` extension was measured separately against red baseline
`d543e598`. Pinned-CPU three-second medians were 148.384 vs 120.815 us/op for
DecodeValidate, 16.114 vs 9.737 us/op for scalar compile, 19.06 vs 16.20 ns/op
for scalar Invoke, and 1,201 vs 964.7 ns/op for warmed scalar Runtime
instantiation, with allocation counts unchanged. The new no-table global egress
path measures 28.74 ns/op with 0 B/op and 0 allocs/op. Warmed no-table
`ref.func`-global instantiation measures 1,082 ns/op, 1,280 B/op, and 9 allocs/op;
its three-function module has an exact 128-byte off-heap descriptor arena.
Null-only globals still allocate no descriptor arena and remain 1,320 B/op and 9
allocs/op. A reverse-order table watchpoint measured table-grow Invoke at 23.17
vs 23.73 ns/op and fixed-table instantiation at 991.3 vs 1,026 ns/op, with
allocations unchanged; the small shifts touch no steady-state table code or
layout and remain scheduler/frequency-noise watchpoints. The implementation adds
no `Instance` or basedata fields and emits the existing 64-bit global load/store
shape.

## Typed calls and signatures

Exported signature conversion preserves `funcref` and `externref` instead of
collapsing them to `i32`. Typed `Instance.Call` checks the exact reference type
and represents a reference in one full-width ABI slot. With reference types
enabled, nullable and store-owned local non-null `funcref` and `externref`
function signatures are executable; incompatible tokens fail closed as described
above, and disabling the feature rejects those signatures explicitly. Externref
and funcref host imports use the synchronous reflection-free slot ABI. Funcref
callbacks see opaque store tokens and results are resolved before re-entry;
non-null host descriptors require an explicit HostFuncRef owner.

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
`referenceStore` is now 88 bytes: the prior 48-byte funcref/instance registry
plus 40 bytes for the externref key, generation seed, and lazy slot slice. It is
allocated once per `Runtime`; its registry is bounded by the store's attached
instances and reuses its map across warmed instantiations. Standalone
scalar/null-only instances keep a nil store, while first non-null
egress lazily creates the private store. Each issued token remains a 24-byte
entry plus two bounded-to-store-lifetime token indexes and intentionally retains
its producer resources until store teardown.

## Multiple funcref tables

Multiple locally defined funcref tables now use one descriptor per table and a
compact arena-backed pointer directory indexed by the Wasm table index. Table 0
retains the existing direct basedata pointer and native load sequence; only
nonzero constant indexes read the directory. The directory pointer reuses the
former unused funcref-descriptor-count basedata slot, so basedata remains 128
bytes and scalar/one-table instances gain no field or allocation.

Active element segments retain their destination table index. Instantiation
allocates every local descriptor before applying active segments in declaration
order, then `table.get`, `table.set`, `table.size`, `table.grow`, `table.fill`,
`table.copy`, `table.init`, and `call_indirect` select the validated descriptor.
Cross-table copy uses memmove semantics after independently checking source and
destination bounds. Passive element descriptors remain shared module-indexed
state and may initialize any compatible local funcref table.

Local table exports now preserve the exact declared export-name-to-index map.
`Instance.ExportedTable` reconstructs a nonzero descriptor only from the bounded,
runtime-owned directory and lazily creates one linked ownership handle per
exported descriptor. Repeated lookup returns the same handle, distinct table
indexes cannot alias, and a missing name fails instead of falling back to table
0. Imported table 0 may still be re-exported, but only under a declared name.
The producer remains subject to the existing close-order contract: close every
consumer before closing the producer whose descriptor/code it executes.

One imported funcref table may now occupy table index 0 before local table
definitions 1..N. The imported descriptor remains in the direct basedata slot,
while every local descriptor is installed in the same bounded directory used by
multiple-local modules. Active elements, indexed operations, cross-table copy,
`call_indirect`, exact imported re-exports, and exact local exports all use the
validated Wasm table index. A consumer's lazy local export handles are prepended
to its ownership chain; close stops at the imported handle, so it cannot release
that handle or unrelated owner-owned handles chained behind it. Consumers still
close before the producer whose table/code they execute.

Failed instantiation keeps the existing finite ownership model because this slice
admits exactly one imported table. If an earlier active segment leaves a failed
instance's local funcref in imported table 0 before a later local-table bounds
failure or start trap, that imported table retains the failed instance until its
finite slots no longer contain the identity or the table owner closes. A shared-
memory importer remains forbidden from also defining local tables because the
local directory/descriptors would overwrite the memory owner's basedata.
`MaxTableEntries` checks the imported declaration minimum and each local minimum
independently.

Multiple imported funcref tables now occupy the leading Wasm table indexes in
exact declaration order. Table 0 remains in the direct basedata slot; imported
table 1 and every later table use the bounded pointer directory. Additional
import metadata is stored in the already-required nonzero table entries, so the
632-byte `Compiled` and 776-byte `Instance` layouts do not grow. `TableImports`
and runtime/module/spec inspection preserve duplicate keys: two declarations of
the same key intentionally resolve to the same `*Table` object, while distinct
keys retain distinct descriptors.

Imported re-exports resolve from the immutable `Imports` map rather than storing
a foreign handle in `Instance.table`. That chain is now reserved for lazy local
export handles, so consumer close cannot traverse or release an owner's unrelated
`Table.next` handles. Failed instantiation scans every finite imported table and
transfers one resource root per distinct retaining handle. Aliased declarations
deduplicate in the table's retained-root map, while distinct tables release their
roots independently; the failed instance closes physically only after the last
retaining table releases it.

`.wago` version 21 preserves multiple imported and local tables in declaration
order, exact element types and import limits, local runtime capacities,
nonzero active destinations, typed passive/declarative state, and exact named
table exports. The loader reconstructs only fresh per-instance descriptors and
compatible-store requirements; it never restores table cell contents or an
owner/store identity.

A capacity-one second funcref table adds exactly 56 off-heap arena bytes relative
to one table: 40 bytes for its header plus entry and 16 bytes for the two-pointer
directory. Active initialization reconstructs descriptors from the bounded
arena directory instead of allocating a Go slice, so warmed one- and two-table
Runtime instantiation both measure 1,224 B/op and 7 allocs/op. On July 10, 2026,
pinned medians were 19.12 ns/op for table-0 `call_indirect`, 18.62 ns/op for table
0 inside a two-table module, 18.63 ns/op for table 1, and 1,092 ns/op for warmed
two-table instantiation; all indirect paths were 0 B/op and 0 allocs/op.

The indexed-export refinement keeps `Instance` at 776 bytes and adds no eager Go
allocation. Each table whose public handle is actually requested allocates one
64-byte `Table`; the linked cache lives in those lazy handles rather than adding
an instance field. Cached table-0/table-1 export lookup medians are 11.01 and
12.92 ns/op at 0 B/op and 0 allocs/op. Warmed two-table instantiation with two
declared exports measures 1,120 ns/op, 1,224 B/op, and 7 allocs/op, matching the
allocation shape without exports. A min-only export reserve is applied only to
the exported local table; an unexported sibling keeps its minimum capacity.

A capacity-one local table after an imported table adds exactly 56 importer-arena
bytes: 40 bytes for its funcref descriptor and 16 bytes for the two-entry pointer
directory. Pinned medians are 20.37 ns/op for imported table-0 indirect dispatch
and 18.47 ns/op for local table-1 dispatch, both 0 B/op and 0 allocs/op. Warmed
instantiation of a minimal imported+local shape is 1,332 ns/op, 1,840 B/op, and 9
allocs/op, matching the imported-only allocation count.

A second imported table adds only the 16-byte two-entry pointer directory; its
descriptor remains foreign-owned. Adding a capacity-one local table after two
imports adds 48 bytes: a 40-byte descriptor plus 8 bytes to grow the directory
from two to three entries. Pinned medians are 21.46 ns/op for imported table 0,
22.23 ns/op for imported table 1, and 20.05 ns/op for local table 2, all 0 B/op
and 0 allocs/op. Warmed two-import-plus-local instantiation is 1,662 ns/op,
1,840 B/op, and 9 allocs/op. Scalar compile remains 26,880 B/op and 62 allocs/op;
`Compiled` remains 632 bytes and `Instance` remains 776 bytes.

Against detached `fc3bea91`, paired pinned medians are 127.073 vs 128.205 us/op
for DecodeValidate, 11.983 vs 12.826 us/op for scalar compile, 18.15 vs 18.49
ns/op for scalar Invoke, 20.61 vs 20.65 ns/op for fixed table-0 indirect, and
1,208/1,237/1,495/1,579 vs 1,231/1,276/1,572/1,662 ns/op for
scalar/fixed/imported/imported+local instantiation. Allocation counts are
unchanged. Timing moved in both directions and includes isolated scheduling
outliers, so it remains a frequency/scheduler watchpoint rather than an
attributed gain.

Against detached `02e75aeb`, pinned medians are 120.588 vs 121.760 us/op for
DecodeValidate, 9.568 vs 10.609 us/op for scalar compile, 17.05 vs 17.50 ns/op for
scalar Invoke, 18.33 vs 19.88 ns/op for fixed table-0 `call_indirect`, 955.5 vs
1,140 ns/op for scalar instantiation, 1,082 vs 1,089 ns/op for fixed-table
instantiation, 1,150 vs 1,147 ns/op for two-local-table instantiation, and 1,382
vs 1,541 ns/op for imported-table instantiation. Allocation counts are unchanged:
DecodeValidate remains 51,354 B/op and 365 allocs/op, compile 26,880 B/op and 62
allocs/op, Invoke paths 0/0, scalar/fixed/two-local instantiation 1,224 B/op and 7
allocs/op, and imported instantiation 1,840 B/op and 9 allocs/op. Broad timing
movement across untouched paths remains scheduler/frequency noise rather than an
attributed improvement.

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

## Snapshots, inspection, and linked teardown

Snapshot products share one fail-closed validator. `Capture`, snapshot marshal,
`LoadSnapshot` and `Instantiate(*Snapshot)` reject
every table and every reference global until a resolver/state format exists. This
also rejects forged in-memory snapshots and raw snapshot blobs embedding a valid
codec-v21 reference module; callers cannot bypass `Capture` to admit unsupported
live state.

`ModuleMetadata` now contains deterministic Wasm-index-ordered `Functions`,
`Globals`, and `Tables`. Function entries carry exact parameter/result reference
types and import/export names. Global entries carry exact type, mutability,
import, and exports. Table entries carry exact type, import, exports, declared
minimum, and an optional exact declared maximum; implementation-only growth
reserves are not reported as Wasm limits. Duplicate table imports remain separate
index entries even when they use the same key. The same metadata is reconstructed
from codec-v21-loaded modules. `Module.Imports` exposes the corresponding exact
function signatures, global types/mutability, and table types/limits.

Cross-link teardown is locked as one ownership proof: a producer may be logically
closed while a consumer still imports a reference global, duplicate aliases of
one funcref table, and an externref table. The consumer continues to call and
observe exact externref identity. Each distinct owner retains one importer root,
duplicate declarations do not multiply it, and closing the final consumer drops
all roots and physically releases the producer. Local exported handles remain
non-owning views; host-created globals/tables retain their existing explicit
close-before-owner contract.

The July 10, 2026 pinned three-second/count-five closeout run measured medians of
115.292 us/op for DecodeValidate, 9.757 us/op for scalar compile, 17.33 ns/op for
scalar Invoke, 19.70 ns/op for fixed table-0 indirect dispatch, 1,090 ns/op for
scalar instantiation, and 1,045 ns/op for fixed-table instantiation. Allocation
watchpoints remain 51,354 B/op and 365 allocs/op, 26,880 B/op and 62 allocs/op,
0/0 for Invoke paths, and 1,224 B/op plus 7 allocs/op for both instantiation
shapes. Scalar marshal/unmarshal medians are 382.3/1,535 ns/op at 336 B/2 and
1,240 B/16; structural-reference marshal/unmarshal medians are 1,364/3,127 ns/op
at 976 B/5 and 2,424 B/36. The inspection and pool checks are off ordinary
compile/invoke/instantiate hot paths, and the local-table explicit-maximum bit
occupies existing struct padding; all documented layouts remain unchanged.

## `.wago` compatibility

Compiled-module codec version 21 keeps WebAssembly structural type codes `0x70`
(`funcref`) and `0x6f` (`externref`) and records an exact byte-sized mask of the
optional core features used by generated code and metadata. Version 20 and older
blobs are rejected by the version-21 loader, and unknown or structurally missing feature bits
fail closed. SIMD blobs additionally reject on hosts without the documented CPU
baseline.

Version 21 serializes reference globals as structure only: exact import/type/
mutability/export metadata plus literal null, imported immutable `global.get`, or
structural `ref.func` initializers. It serializes all compiled tables in Wasm index
order with exact element type, import key/limits or local runtime size/capacity,
the local declaration's explicit-maximum bit, initializer, and named exports. Active and element-state metadata preserve exact
reference type, mode, destination table, offset, and explicit null/`ref.func`
payloads. Loaded modules allocate fresh cells, descriptors, and typed table
storage, and focused tests execute reference globals, two funcref tables,
nonzero-table `call_indirect`, exact table exports, and externref table null round
trips after a codec load.

The codec records whether imported calls use the binding-independent dispatch
ABI, but never writes a live `FuncRef`/`ExternRef` token, descriptor address,
producer pointer, `HostFuncRef` owner or dispatch index, thunk address, concrete
import target, table cell contents, or reference-store identity. Nonzero reference
literal bits are rejected before marshal/load. Function-import modules are now
serializable before binding; instantiation creates fresh dispatch cells and host
thunks in the target runtime. Decoding uses a fresh `Compiled` value, so reused
receivers cannot retain stale tables, exports, globals, passive state, or runtime caches.
Snapshots remain deliberately stricter: every table and every reference global is
rejected until an application-provided resolver/state format exists.

### Imported-call/context cleanup measurements (July 22, 2026)

On the pinned AMD Ryzen 7 8845HS cleanup run, `Compiled` is 584 bytes, down from
632 bytes (48 bytes / 7.6%) after deleting retained link source, deferred-link
flags, worker/bounds recompile policy, synchronous-link state, and host-link
caches. Modules with function imports now contain complete native code and share
one executable mapping across differently bound instances. Canonical funcref
descriptors grow from 32 to 40 bytes only when reference operations require that
arena; table entries remain 32 bytes. The trailing context pointer makes indirect
calls distinguish instances that share the same linear-memory base.

Three one-second samples measured scalar instantiation at 1,328–1,338 ns/op,
1,360 B/op, and 9 allocs/op (about 9.7% slower than the 1,196–1,231 ns/op
pre-dispatch samples, with bytes and allocations unchanged). Direct returning
host calls measured 123.0–128.8 ns/op at 0 B/op; direct cross-instance calls
41.2–43.0 ns/op at 0 B/op; host funcref table calls 121.1–121.9 ns/op at 0 B/op;
and legacy void host replay 57.5–58.0 ns/op at 8 B/op and 1 allocation. Direct
host-import instantiation measured 10.9–11.2 us/op, 2,032 B/op, and 19 allocations;
the table/funcref form measured 12.0–12.6 us/op, 2,040 B/op, and 20 allocations.
Instantiation now sizes the off-heap arena from concrete bindings instead of the
worst-case async-host shape: a synchronous host module requests 65,216 fewer
bytes and a cross-instance-only module requests 65,544 fewer bytes. The warmed
direct-host timing remained 11.1–11.3 us/op with unchanged Go bytes/allocations.
A 1,600-function imported-call compile on one pinned CPU measured 48.5–51.3 ms,
4,016,276–4,016,286 B/op, and 15,996 allocations across worker policies.

Pinned single-CPU three-second medians on July 10, 2026 are 483.8 ns/op and
1,724 ns/op for scalar marshal/unmarshal (336 B/op with 2 allocations and
1,240 B/op with 16 allocations). The structural reference fixture measures
1,478 ns/op marshal at 976 B/op and 5 allocations, and 4,103 ns/op unmarshal at
2,424 B/op and 36 allocations. In the same run, DecodeValidate measured
118.659 us/op, scalar compile 11.627 us/op, scalar Invoke 16.23 ns/op, fixed
table-0 indirect dispatch 19.37 ns/op, scalar instantiation 1,310 ns/op, and
fixed-table instantiation 1,376 ns/op. Ordinary allocation watchpoints remain
51,353-51,354 B/op and 365 allocations for validation, 26,880 B/op and 62
allocations for compile, zero allocations for Invoke paths, and 1,224 B/op plus
7 allocations for scalar/fixed-table instantiation. After removing retained link
source, link-policy fields, and host-link caches, `Compiled` is 584 bytes;
the broad instantiation timing movement without allocation/layout change remains
a scheduler/frequency watchpoint rather than an attributed codec regression.

The focused commands are:

```sh
go test -count=1 -run '^TestCompiledCodecV21' ./src/wago

taskset -c 0 go test ./src/wago -run '^$' \
  -bench '^(BenchmarkMarshalCompiledSmallScalar|BenchmarkUnmarshalCompiledSmallScalar|BenchmarkMarshalCompiledStructuralReferences|BenchmarkUnmarshalCompiledStructuralReferences)$' \
  -benchmem -benchtime=3s -count=5
```

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
module sites and all three invalid assertions on both validators. The complete
Release 2 validation gate is now green at 1,600 passed / 0 failed / 0 skipped
modules and 2,880 passed / 0 failed / 1,077 skipped assertions. A paired
pinned-CPU `BenchmarkDecodeValidate` run of the 256-function
fixture measured a median of 114.821 us/op at the pre-rule `f8d20081` baseline
and 113.725 us/op after the focused proof commits, with both runs at 51,358 B/op
and 365 allocs/op. The declaration check therefore adds no measured allocation
or compile-latency regression in that watchpoint.

## Release 2 execution harness

The official-suite harness encodes null references as token zero. It interns
WABT `ref.extern N` arguments in the target instance's reference store and
checks externref results by resolving the returned token to the same fixture
identity. WABT's value-less non-null funcref expectation matches any nonzero
opaque token, while null still requires token zero. Direct `get` actions execute
externref and funcref globals through typed access. No reference result/global
sites remain classified as harness gaps.

With WABT 1.0.36 available on July 10, 2026, the Release 2 execution harness
honors named modules, `register`, named actions, and `assert_uninstantiable` with
registered function, memory, table, and global imports. Every file replay uses
one `Runtime`, one file-scoped standard table, one file-scoped standard memory,
and the exact standard print functions. Imported function and memory re-exports
preserve their original owners. The current command reports 1,600 passed / 0
failed / 0 skipped modules and 48,248 passed / 0 failed / 0 skipped assertions;
every bounded gap reason is zero, and the test now fails if any Release 2 module
or assertion becomes skipped. `imports.wast` is fully green at 54 modules / 34
assertions, `data.wast` at 25 / 14, and `linking.wast` at 21 / 90. The complete
valid-module validation gate remains 1,600 passed / 0 failed / 0 skipped;
invalid/malformed assertions retain their independent 2,880 passed / 0 failed /
1,077 non-validation-action skips.
