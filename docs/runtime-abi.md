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
loads this pointer from basedata, then indexes the per-instance global slot
array.

## Global storage convention

Each instantiated module owns an arena-backed globals byte slice:

- one 8-byte slot per wasm global, in wasm global-index order;
- imported global slots come before locally defined global slots;
- `i32` and `f32` values occupy the low 32 bits of the slot; backend loads and
  stores use 32-bit accesses for these low halves;
- `i64` and `f64` values occupy all 64 bits of the slot; backend loads and
  stores use 64-bit accesses for the full slot;
- the slot array is instance-local mutable state and is released with the
  instance arena on `Instance.Close`.

The globals pointer installed in `JobMemory` points at off-heap arena memory with
a stable address. Native code must not receive Go heap pointers for globals, and
per-access `global.get`/`global.set` code must not allocate.
