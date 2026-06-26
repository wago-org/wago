# Runtime ABI Notes

This document records runtime layout details that native code relies on. Keep it
in sync with `src/core/runtime/basedata_test.go`, `src/core/runtime/abi`, and
backend code that emits loads from `JobMemory` basedata.

## JobMemory basedata

`JobMemory` reserves basedata immediately before the linear-memory base. Offsets
are addressed as negative displacements from the linear-memory pointer used by
JIT code. Existing offsets must not move without re-deriving the runtime ABI and
updating the guard tests.

The globals pointer lives at basedata offset `88` (`abi.GlobalsPtrOffset`, used
by runtime layout and backend codegen). Backend `global.get`/`global.set` code
loads this pointer from basedata, indexes the per-instance global pointer table,
then loads or stores the pointed-to 8-byte global cell.

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
