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
context before execution. Shared memories serialize native entry while rebinding,
so multiple importers may retain independent globals, tables, host context, and
passive-segment state without leaving basedata pointing at another instance's
released arena. Linear-memory size/growth caches remain backing-owned, while trap
and stack fields remain invocation-owned.

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
public token still retains the importer's descriptor arena.

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

The deferred host-call model (host imports are logged during execution and
replayed only after the wasm call returns) guarantees no host or cross-instance
access occurs *within* a single execution. Intra-instance spill discipline is
therefore both sufficient and necessary; non-exported, non-imported globals need
only that, while exported and imported globals additionally must be coherent at
`Invoke` return, which a function-exit spill already provides.
