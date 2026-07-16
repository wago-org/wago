# Runtime ABI Notes

This document records runtime layout details that native code relies on. Keep it
in sync with `src/core/runtime/basedata_test.go`, `src/core/runtime/abi`, and
backend code that emits loads from `JobMemory` basedata.

## JobMemory basedata

`JobMemory` reserves basedata immediately before the linear-memory base. Offsets
are addressed as negative displacements from the linear-memory pointer used by
JIT code. Existing offsets must not move without re-deriving the runtime ABI and
updating the guard tests.

The globals pointer lives at basedata offset `112` (`abi.GlobalsPtrOffset`, used
by runtime layout and backend codegen). Backend `global.get`/`global.set` code
loads this pointer from basedata, indexes the per-instance global pointer table,
then loads or stores the pointed-to 8-byte global cell.

The trap-cell pointer and stack fence are per-invocation control fields, not
per-instance constants. Cross-instance direct and indirect calls temporarily
copy the active caller's values into the callee basedata so a callee trap unwinds
the whole native call tree. Consequently every later public entry—including the
prepared-call fast path—must restore its own trap-cell pointer and engine fence
before entering native code. Bind-once prepared calls are valid only while an
instance can never be used as a cross-instance callee.

The remaining pointer fields are modeled as `runtime.InstanceContext` and
captured when instantiation finishes. Every public native entry rebinds that
context and refreshes its invocation control fields before execution. The
current correctness-first execution lease serializes native execution
process-wide: one public root owns every basedata region its direct or indirect
cross-instance call graph may rebind. This avoids recursive per-memory lock
ordering and covers same-memory, different-memory, and cyclic call graphs.
Linear-memory size/growth caches remain backing-owned, while trap and stack
fields remain invocation-owned.

Direct imported calls load `{entry, homeLinMem, targetContext, callerContext}`
from the per-instance dispatch table. Indirect calls recover `targetContext` from
the canonical funcref descriptor referenced by the table entry. Both paths bind
the target pointer context before crossing instances and restore the caller
context after a normal return. A trap unwinds the native call tree before restore;
the next serialized public entry always rebinds its own captured context first.
Canonical funcref descriptors are 40 bytes: the 32-byte table payload plus an
8-byte owning-context pointer. Table entries remain 32 bytes. A function importer
retains each distinct producer instance until the importer's physical resource
release; logical close alone cannot release those roots when a table, global, or
public token still retains the importer's descriptor arena. Imported HostFuncRef,
reference-global, and table attachments follow the same physical-release rule.
When an imported funcref container retains the writer itself, that container
attachment is transferred to the retained root to avoid a self-owning importer
cycle, and is released exactly once.

## Synchronous host parking and active-callee routing

The trap allocation is 16 bytes. Bytes 0..3 hold the trap/pending code; bytes
8..15 hold the exact active host-control-frame pointer for a parked activation.
AMD64 and ARM64 host stubs save the current register state into the callee's
control frame, publish that frame pointer at `trap+8`, write
`hostCallPending`, and unwind to Go.

`Engine.CallWithHost` reads arguments and import indexes from the published
frame, not from the public root's frame. The frame address resolves to the
physically live callee instance, so dispatch uses that instance's import
namespace, HostFunc/HostFuncRef binding, HostModule, and result buffer. Every
synchronous frame receives its trampoline at instantiation, including callees
that have never been entered as public roots.

The process-wide native execution lease is released before arbitrary Go host
code runs. Nested Wasm entry may therefore acquire the lease without deadlock.
On every normal return or panic path the dispatcher reacquires the lease. It
rebinds the exact parked callee context when the execution epoch shows that a
nested or competing public entry ran; otherwise the already-installed context
is reused. Immediately before `resumeNative`, the runtime restores the parked
activation's trap pointer, stack fence, and architecture-specific trap re-entry
control. `HostExit`, traps, and propagated host panics therefore cannot leave a
lease held or resume against a context installed by nested entry.

Instances with actual host bindings use synchronous dispatch, including legacy
void HostFunc signatures, because such an instance may later execute as a
cross-instance callee. That parking capability propagates transitively through
canonical InstanceExport imports. Where the architecture host policy permits,
a host-free cross-instance-only consumer whose targets cannot park uses the
ordinary native entry path and allocates neither a control frame nor an async
replay log. Forced-synchronous architectures still allocate their control frame.
Modules importing a funcref table remain
synchronous because the table may be mutated to contain a host descriptor. The
old async log format remains an internal compatibility path but is not selected
for these compositions.

The synchronous parked-Go transition restores callee-saved GPRs, but System V XMM
registers are caller-saved. Before any synchronous host or internal GC helper
call, amd64 copies all arguments to the 328-byte control frame and then spills
dirty pinned locals to their canonical frame slots. Float locals reload lazily
after resume. Codegen must not assume an XMM-pinned local survives parked Go
merely because RBX/RBP/R12-R15 do.

## Guarded host memory access

In guard-page mode, `memory.grow` raises the logical size before newly in-bounds
pages are necessarily committed; native loads/stores commit them lazily through
the fault handler. Host access uses `JobMemory.HostBytesChecked`, which mprotects
and extends the stable-base Go view through the current logical size first. This
is required for `Memory.Bytes`, typed host reads/writes, snapshot restore, and
active data initialization against an imported memory that grew before the new
instance was created. `CurrentBytes` remains limited to the original committed
Go slice and must not be used for that case.

On ARM64, the guard fault handler passes the faulting linear-memory base through
saved `X9` when it replaces the faulting PC with the native trap-exit landing
pad. The landing pad must not depend on the platform signal trampoline restoring
Wasm's pinned `X26`: Linux and Darwin replacement-PC returns can otherwise reach
the landing pad with an unusable `X26`, preventing recovery of the foreign-stack
save area and `enterNative` continuation.

ARM64 modules whose declared or imported memory minimum is zero use explicit
bounds checks and classic growable memory even when signals-based checks were
requested. This narrow fallback preserves exact zero-page semantics until the
ARM64 guard entry can safely place its control words immediately below a linMem
that starts on the first inaccessible linear page. One-page-and-larger ARM64
memories continue to use the guard-page path.

## Global storage convention

Each instantiated module owns an arena-backed globals pointer table:

- one 8-byte pointer-table entry per wasm global, in wasm global-index order;
- imported global entries come before locally defined global entries;
- duplicate imports of the same global key point at the same host-owned global
  cell, preserving mutable global object identity;
- locally defined globals point at instance-local 8-byte cells released with the
  instance arena on `Instance.Close`;
- `i32` and `f32` values occupy the low 32 bits of a cell; backend loads and
  stores use 32-bit accesses for these low halves;
- `i64` and `f64` values occupy all 64 bits of a cell; backend loads and stores
  use 64-bit accesses for the full cell.

The globals pointer table and every global cell handed to native code live in
stable off-heap memory. Native code must not receive Go heap pointers for
globals, and per-access `global.get`/`global.set` code must not allocate.

## WasmGC heap pointer stability

`gc.Ref` values are stable compact integers, but the current Throughput WasmGC
heap stores object payloads in Go byte slices. Growing that heap may allocate a
new backing slice and copy existing bytes. Generated native code therefore must
not cache raw pointers into WasmGC object payloads across helper calls,
allocations, safepoints, or any operation that can grow/collect the GC heap.

Until the Throughput allocator moves to chunked pages or pre-reserved backing
memory with stable native addresses, WasmGC object access from generated code is
helper-call-only: pass `gc.Ref` values and field/element indexes to runtime
helpers, then discard any transient Go-derived address before returning to
native code. Direct native loads/stores may be introduced only after the
allocator and runtime ABI explicitly guarantee payload address stability.

Helper calls that may allocate, collect, or run barriers must publish all
caller-known live refs, not just direct helper arguments. The `codegen.Emitter`
root protocol is all-or-nothing: `SpillLiveRefs` prepares storage,
`PublishRoots` either fully publishes it or returns an error with no roots live,
and successful publication must be followed by exactly one `UnpublishRoots` even
if the runtime helper fails.

Iterations 38-39 have a narrower executable helper ABI while general native root-map
publication remains incomplete. Exact linux/amd64 explicit-bounds numeric-struct products
park through the existing 328-byte synchronous control frame with dispatch bit 30 reserved
for internal GC helpers. The helpers receive compact `gc.Ref` values, type/field indexes,
and numeric bits in copied scalar slots; they never expose or retain a Go-slice payload
address in native code.

Allocation-local products pass the non-nil zero-sized `gc.EmptyRoots` set only when no prior
frame/local ref is live. Iteration 39 separately admits at most two immutable numeric GC
globals. Instantiation allocates one object, initializes it, installs a checked collector
`GlobalSlot`, and only then begins the next initializer. The native 8-byte global cell and
the collector slot contain the same compact stable handle; a fixed two-entry lazy instance
sidecar records their index mapping. Because these globals are immutable, no cell/slot write
coherence or barrier is required. Any mutable GC global must update both transactionally and
call the collector slot barrier, so it remains rejected.

Ordinary/packed `struct.get` and numeric `struct.set` do not collect. The official basic
numeric actions cross only exact internal callees whose reachable instructions cannot
allocate. Iteration 40 owns that product's sole exported non-null `ref.struct` result only
after native return: the compact 32-bit handle is checked against the declared result and
exact dynamic struct type, replaced with a random opaque store token whose upper 32 bits are
non-zero, and rooted in one reusable checked collector global slot. A second live token per
producer rejects. Release nulls the slot through the collector barrier contract; the token
retains the producer collector across logical instance close and supports both close orders.
Helper calls, token root mutation, and collector close serialize through one lazy per-instance
mutex.

This result-token root is not a native frame root and does not justify widening `gc.EmptyRoots`.
It exists only after the Wasm activation has returned, carries no cached payload pointer, and
is compile-only exact-product state absent from codec v27.

Iterations 41-42 give pointer-free arrays a separate compile-only helper/product path. Exact
`array.new`, `array.new_default`, bounded `array.new_fixed`, and i8 `array.new_data` producers
perform one allocation with a proven empty live-frame-ref set. Helper-side `array.get`, numeric
or packed `array.set`, and `array.len` discard every transient Go-derived address before native
resume. The fixed leader's one immutable global and the default leader's two immutable globals
install checked collector slots immediately after each object's scalar initialization; the first
default root is visible before the second allocation can collect. Numeric/packed array stores
need no barrier. Public results are tokenized only after native return and use the same one-live-
token root/lifetime policy with an exact dynamic array-type check.

`array.new_data` reads only the copied scalar source/length/type/segment arguments plus the
per-instance 16-byte passive descriptor. Parked Go checks the current descriptor length and
u64-widened source end before allocating, then copies retained immutable bytes into i8 payload
slots. `data.drop` mutates only the descriptor length. Thus non-empty post-drop and overflowing
ranges trap before collector state changes, while source zero/length zero remains valid; no
Go-slice pointer enters native state or survives helper return.

None of these paths permits a second allocation while a transient ref is live. Reference
elements, passive-element GC roots, object/bulk barriers, non-null ingress, mutable globals,
hosts, snapshots, guard mode, and arm64 remain closed. These exact struct/array proofs must not
be treated as the general `codegen.Emitter` publication protocol.

## Global coherence invariant

The global cell is the sole host- and cross-instance-visible storage for a
global. Backend code currently reads or writes the cell on every `global.get`
and `global.set`, so the cell is always authoritative.

A future backend may cache global values in registers across straight-line code.
Such caching must preserve this invariant:

- spill cached values back to the cell at function return and around calls
  (host imports and wasm-to-wasm calls), and reload after, so callers and later
  `Instance.Global`/`SetGlobal` reads observe writes;
- never assume exclusive ownership of an imported global's cell — its identity
  may be shared with the host and with other instances importing the same
  `*Global`, so the cell must remain the shared source of truth.

Synchronous host callbacks and cross-instance calls may observe globals during
one public invocation. Generated code must therefore spill caller-cached globals
before either boundary and reload afterward. Non-exported, non-imported globals
need the same call-boundary discipline for host re-entry; exported and imported
globals additionally remain coherent at public return.
