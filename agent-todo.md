# Agent Todo

## WebAssembly 2.0 Completion Roadmap

Goal: complete WebAssembly 2.0 semantics without regressing the table-0,
call, compile, instantiation, or memory-footprint hot paths.

WebAssembly 2.0 scope for this roadmap:

- sign-extension operations;
- non-trapping float-to-int conversions;
- multi-value functions and blocks;
- reference types (`funcref` and `externref`);
- typed `select`;
- table operations and multiple tables;
- bulk memory/table operations; and
- core SIMD.

Tail calls, typed function references, GC, exceptions, memory64, and
multi-memory are not required for WebAssembly 2.0 completion.

## Current State

- [x] Sign-extension operations.
- [x] Non-trapping float-to-int conversions.
- [x] Multi-value semantics. The optimized multi-result register ABI remains a
  performance task, not a WebAssembly 2.0 semantics blocker.
- [x] Typed `select` for currently executable value types.
- [x] Bulk linear-memory operations and passive data segments.
- [x] Table operations on table 0: `table.get`, `table.set`, `table.size`,
  `table.grow`, `table.fill`, `table.copy`, `table.init`, and `elem.drop`.
  Min-only local funcref tables that execute `table.grow` or export the table
  reserve a bounded 64-entry growth window; fixed-use min-only tables retain
  their minimum-sized descriptor footprint.
- [x] Passive and declarative funcref element handling for table 0.
- [x] Core SIMD for the documented linux/amd64 baseline.
- [x] Funcref table initializer expressions, including non-null `ref.func`.
- [x] Nullable funcref parameters/results, zero-initialized locals, direct calls,
  block results, `ref.null`, and `ref.is_null`; the Release 2 execution harness
  now executes null funcref arguments/results instead of counting them as gaps.
- [x] Public local `ref.func` results use stable runtime/private-store-owned
  opaque tokens through raw `Invoke`, typed `Call`, and `invokeLocal`. Same-store
  tokens translate back only after exact validation; forged, cross-runtime, and
  cross-private-store tokens fail before native entry. Token retention keeps the
  producer's descriptor arena, code mapping, and home context alive after its
  logical `Instance.Close` until store teardown.
- [x] Canonicalize same-runtime `InstanceExport` funcrefs: imported `ref.func`
  and canonical local descriptors returned by `table.get` reuse the producer's
  opaque identity, retain the true producer, and survive producer logical close.
  Cross-runtime/private-store imports, corrupted `refSlot` metadata, and host
  imports fail closed without issuing tokens.
- [ ] Broaden public funcref tokens to host descriptors and remaining
  cross-instance/reference-global boundaries; these remain fail-closed.
- [x] Measure the token foundation: scalar, null, local egress, imported egress,
  and same-runtime round trips remain 0 B/op and 0 allocs/op. Stable medians are
  16.23, 20.59, 28.42, 43.26, and 35.39 ns/op respectively. Warmed Runtime
  instantiation remains 1,224 B/op and 7 allocs/op. Instance size is 776 bytes
  (+32 from `e54f9556`); `referenceStore` is 48 bytes (+8 for the bounded live-
  instance registry map header), while standalone scalar/null-only instances
  keep the private store lazy.
- [x] Measure bounded min-only table growth: successful null `table.grow` has a
  stable median of 22.77 ns/op with 0 B/op and 0 allocs/op. Warmed Runtime
  instantiation medians are 1,001 ns/op for a fixed-use min-only table and
  1,010 ns/op for the 64-entry growth-capable shape, both 1,224 B/op and 7
  allocs/op; the detached `08476b11` baseline measured 998.8 and 1,006 ns/op.
  The growth reserve adds 2,048 off-heap descriptor bytes for a min=0 funcref
  table and no Go allocation; fixed-use min-only capacity is unchanged.
- [ ] Full first-class `funcref` support.
- [ ] Executable `externref` support.
- [ ] Multiple tables.
- [x] The Release 2 `table_grow.wast` min-only funcref growth assertions now
  pass: growth from 10 to 20 returns the old size and leaves every new slot null.
- [ ] WebAssembly 2.0 conformance gate with no feature-related skips. With WABT
  1.0.36 available, the July 9, 2026 execution run reports 1,403 passed / 197
  skipped modules and 46,234 passed / 2 failed / 1,978 skipped assertions. The
  two current execution failures are the `linking.wast` shared-memory/table
  assertions; feature-related compile, instantiate, reference-value, and
  unavailable-module gaps remain explicit.

The feature documentation is stale where it still describes table operations,
passive element execution, or multi-value semantics as incomplete.

## Implementation Order

### P0 — Pin and Wire the Official WebAssembly 2.0 Suite

- [x] Add a separately pinned official WebAssembly 2.0 testsuite revision rather
  than replacing the pre-reference-types WebAssembly 1.0 conformance baseline.
- [x] Update the validation and execution harnesses for the 2.0 core-suite
  layout.
- [x] Install or provision `wast2json` in CI for the 2.0 job.
- [ ] Make valid modules rejected as unsupported fail the 2.0 job.
- [ ] Make invalid modules accepted by the decoder/validator fail the job.
- [ ] Add reference-valued assertion argument and result support.
  - [x] Encode, invoke, and assert null `funcref` arguments/results as token zero.
  - [ ] Add non-null funcref identity and externref values after their ownership
    models are implemented.
- [ ] Stop treating reference arguments, reference results, or reference globals
  as out-of-scope skips in `src/wago/spectest_exec_test.go`.
- [x] Record per-file module/assertion pass, fail, and skip counts.
- [x] Classify execution skips with bounded compile, instantiate, blocked-module,
  absent-export, reference-argument, reference-result, and reference-global
  reason counts, and expose those counts in the CI card.

Completion criterion: the harness reports every remaining WebAssembly 2.0 gap
explicitly instead of hiding it behind unsupported-module or reference-value
skips.

### P1 — WebAssembly 2.0 Validation Correctness

- [ ] Enforce the declared-function-reference rule for `ref.func`; the current
  validator checks the function index but not whether the reference is declared.
- [ ] Validate `funcref` and `externref` in function params/results, locals,
  globals, block signatures, typed `select`, tables, and element segments.
- [ ] Validate multiple-table indexes for `call_indirect`, active elements, and
  all table instructions.
- [ ] Validate element-segment and table reference-type compatibility.
- [ ] Validate `ref.null`, `ref.func`, and `ref.is_null` in every WebAssembly 2.0
  context.
- [ ] Drive additional validation fixes from the official 2.0 invalid and
  malformed corpus.

Keep decode and validation strict. Do not turn malformed structured sections or
invalid proposal encodings into best-effort parsing.

### P2 — Public Reference Types and Slot ABI

- [x] Add public `ValFuncRef` and `ValExternRef` value types.
- [x] Add opaque `FuncRef` and `ExternRef` public representations.
- [x] Add typed constructors/accessors for reference-valued `Value`s.
- [x] Define the low-level `uint64` representation as an opaque reference token,
  never a documented Go or native pointer.
- [ ] Update value-type encoding, `.wago` type metadata, signatures, reflection-
  free host calls, and typed `Call` validation.
  - [x] Preserve reference value types in public signatures and typed `Call`
    one-slot validation/result decoding.
  - [x] Add codec-version-18 structural signature type codes while rejecting
    reference globals and live reference tokens on marshal/load.
  - [ ] Enable reference values at reflection-free host-call boundaries when P3
    and P5 make funcref/externref execution available.
- [x] Define null construction and testing in the public API.

Suggested API direction:

```go
type FuncRef struct { /* opaque */ }
type ExternRef struct { /* opaque */ }

func ValueFuncRef(FuncRef) Value
func ValueExternRef(ExternRef) Value
func (Value) FuncRef() FuncRef
func (Value) ExternRef() ExternRef
```

### P3 — First-Class Funcref Execution

- [x] Execute the nullable funcref foundation through parameters/results,
  zero-initialized locals, direct calls, block results, `ref.null`, and
  `ref.is_null`, with exact typed `Call` values and feature gating.
- [ ] Permit `funcref` in function parameters, results, locals, and block
  parameters/results in the frontend support pass.
- [ ] Carry funcref through direct calls, recursion, multi-value returns,
  branches, typed `select`, and spills as a 64-bit JIT value.
- [ ] Carry funcref through cross-instance calls and synchronous host imports.
- [ ] Return and accept runtime-owned non-null funcref tokens through `Invoke`
  and typed `Call` without exposing descriptor addresses.
  - [x] Issue stable opaque tokens for local `ref.func` descriptors, validate
    same-store ingress, retain producer resources, and give standalone
    `Instantiate` a lazy private-store policy.
  - [x] Translate same-runtime cross-instance imported funcrefs through exact
    `InstanceExport`, descriptor-range, and `refSlot` canonicalization checks.
  - [ ] Translate host funcrefs and broader cross-instance/global boundaries;
    keep them fail-closed until their owners and close ordering are proven.
- [ ] Zero-initialize funcref locals.
- [ ] Audit every scalar/non-`v128` assumption in call marshalling, result
  handling, codecs, and snapshots.
- [ ] Preserve descriptor identity for `ref.is_null` and any supported identity
  operations.

The backend already maps reference values to a 64-bit machine type in several
places. Reuse that representation rather than adding a parallel register class.

### P4 — Funcref Globals and Lifetime

- [ ] Add 8-byte funcref global cells.
- [ ] Support local, imported, exported, mutable, and cross-instance funcref
  globals.
- [ ] Support `ref.null` and valid `ref.func` global initializers.
- [ ] Support imported immutable `global.get` initializers where the 2.0 rules
  permit them.
- [ ] Add host constructors and accessors for funcref globals.
- [ ] Keep funcref globals out of numeric-only optimizations unless explicitly
  proven safe.
- [ ] Define funcref ownership so a reference returned to the host or stored in
  another instance cannot become a dangling pointer when the producing instance
  closes.
- [ ] Ensure retained funcrefs also retain the required code mapping and home
  instance context.

Do not expose the current pointer into an instance descriptor arena as the public
funcref identity.

### P5 — Externref Store and Host ABI

Implement externrefs as handles, not pointers in mmap-backed Wasm storage.

- [ ] Reserve handle zero for null.
- [ ] Add generation checking to detect stale handles.
- [ ] Add a runtime/store-owned Go table mapping handles to embedder objects.
- [ ] Keep native code limited to copying/testing the 64-bit handle.
- [ ] Translate handles only at public API and host-call boundaries.
- [ ] Define whether standalone `Instantiate` creates a private store.
- [ ] Share one store among instances created by the same `Runtime`.
- [ ] Reject or explicitly bridge externrefs passed between incompatible stores.
- [ ] Retain registered externrefs for the store lifetime unless a sound,
  measured reclamation scheme is implemented.
- [ ] Release the store and its Go roots on `Runtime.Close`.
- [ ] Cover host functions that accept, return, and round-trip externrefs.

Avoid a process-global unbounded cache. A per-runtime/store table makes the
lifetime and memory bound explicit.

### P6 — Externref Globals and Tables

- [ ] Add externref globals using 8-byte handle cells.
- [ ] Support null externref constant expressions.
- [ ] Add imported/exported/mutable externref globals and host accessors.
- [ ] Add externref tables with 8-byte entries rather than reusing the 32-byte
  funcref call-descriptor layout.
- [ ] Support externref `table.get`, `table.set`, `table.size`, `table.grow`, and
  `table.fill`.
- [ ] Support compatible `table.copy`, `table.init`, and `elem.drop` behavior.
- [ ] Preserve null and opaque identity across locals, calls, globals, tables,
  imports, and exports.
- [ ] Require a compatible externref store when sharing an externref table across
  instances.

### P7 — Generalize Element Metadata

Replace funcref-table-0-specific metadata with typed, table-indexed element
metadata.

- [ ] Store the destination table index for active segments.
- [ ] Store the segment reference type.
- [ ] Represent active, passive, and declarative modes explicitly.
- [ ] Represent `ref.null` and `ref.func` element expressions without conflating
  null with an ordinary function index.
- [ ] Support typed externref segments; WebAssembly 2.0 module expressions can
  initialize them with null references.
- [ ] Keep per-instance drop state for passive segments.
- [ ] Preserve correct instantiation-time bounds traps and all-or-nothing
  initialization behavior.
- [ ] Update `table.init`, `table.copy`, active initialization, validation,
  footprint accounting, and serialization.

A possible metadata direction is:

```go
type ElemInit struct {
    TableIndex uint32
    RefType    ValType
    Mode       ElemMode
    Values     []RefInit
}
```

### P8 — Multiple Tables

Preserve the current table-0 fast path while adding a table directory.

- [ ] Replace `Compiled.HasTable`, `TableSize`, `TableMax`, and the single table
  import fields with per-table metadata.
- [ ] Replace `Instance.tableDesc` with indexed table handles/descriptors.
- [ ] Retain the existing basedata table-0 pointer for immediate table index 0.
- [ ] Add a basedata table-directory pointer and count for nonzero indexes.
- [ ] Compile table index 0 to the current direct load sequence.
- [ ] Compile nonzero constant indexes to a directory lookup.
- [ ] Remove `readSingleTableIndex` and `readTablePairIndexes` restrictions.
- [ ] Support indexed `table.get`, `table.set`, `table.size`, `table.grow`,
  `table.fill`, `table.copy`, and `table.init`.
- [ ] Support nonzero-table `call_indirect` with the correct element type and
  signature checks.
- [ ] Support active element segments targeting any declared table.
- [ ] Support combinations of imported and locally defined tables.
- [ ] Resolve table exports by name instead of treating the name as advisory.
- [ ] Update host-created tables to carry element type, entry stride, limits,
  ownership, and externref-store identity.
- [ ] Update table policy limits to account for all tables, with clearly defined
  per-table and/or aggregate semantics.
- [ ] Update instantiation-arena footprint checks for heterogeneous table entry
  sizes.

Preferred runtime shape:

- table 0 remains directly addressable through the existing basedata slot;
- a compact pointer directory handles tables 1..N;
- funcref tables use the current 32-byte call descriptors;
- externref tables use 8-byte handles.

### P9 — Codec, Snapshots, Pools, and Product Surface

- [ ] Bump the `.wago` codec version for reference types and per-table metadata.
- [ ] Serialize reference value types, table definitions/imports/exports,
  element metadata, and required feature bits.
- [ ] Continue to serialize only module structure and null/reference-function
  initializers, not live host externref objects.
- [ ] Explicitly reject snapshots containing live externrefs until an
  application-provided resolver is designed.
- [ ] Audit instance reset/pooling so tables, reference globals, passive element
  state, and externref-store bindings cannot leak between tenants.
- [ ] Audit cross-instance links and close ordering for reference ownership.
- [ ] Update module inspection APIs to report all tables and reference types.

### P10 — Feature Reporting and Documentation

- [x] Add `CoreFeatureSIMD` to `CoreFeaturesV2` so the public feature group matches
  the WebAssembly 2.0 release scope.
- [ ] Keep reference-type subfeatures behind `CoreFeatureReferenceTypes` until
  the complete 2.0 subset is executable.
- [ ] Decide whether `SupportedFeatures` should report partial families; prefer
  not to claim complete reference-types support while valid 2.0 modules are
  rejected.
- [ ] Update `FEATURES.md` to mark table bulk operations and passive elements as
  implemented for table 0, while clearly listing externref and multiple-table
  gaps.
- [ ] Update `ROADMAP.md` and `README.md` so multi-value semantics are not called
  incomplete solely because the optimized ABI is pending.
- [ ] Document reference token/store lifetime and cross-runtime restrictions.
- [ ] Publish exact WebAssembly 2.0 conformance counts when complete.

### P11 — Conformance and Performance Gate

- [ ] Run the complete official WebAssembly 2.0 decode/validation corpus.
- [ ] Run all applicable execution assertions with reference arguments/results
  enabled.
- [ ] Require zero feature-related module and assertion skips.
- [ ] Add focused tests for:
  - [ ] undeclared `ref.func` rejection;
  - [ ] funcref identity and null behavior;
  - [ ] externref host round trips;
  - [ ] reference locals, globals, params, results, and multi-value returns;
  - [ ] multiple local/imported/exported tables;
  - [ ] cross-table copy and overlap semantics;
  - [ ] nonzero-table `call_indirect`;
  - [ ] instantiation bounds traps;
  - [ ] cross-instance reference ownership and close ordering;
  - [ ] stale externref-handle rejection.
- [ ] Benchmark and report before/after numbers for:
  - [ ] table-0 `call_indirect`;
  - [ ] table-0 get/set/grow/fill/copy/init;
  - [ ] ordinary scalar direct calls;
  - [ ] compile latency;
  - [ ] instantiation latency;
  - [ ] zero-table and one-table instance footprint;
  - [ ] funcref versus externref table bytes per entry;
  - [ ] host calls with and without reference values.

## Definition of Done

Wago can claim WebAssembly 2.0 support when all of the following are true:

- [ ] Every Release 2.0 feature family is decoded, validated, executable, and
  feature-gated correctly.
- [ ] `funcref` and `externref` work in signatures, locals, globals, control flow,
  host calls, and tables.
- [ ] Multiple tables work for definitions, imports, exports, active elements,
  table operations, and `call_indirect`.
- [ ] The official WebAssembly 2.0 validation and execution corpus has no
  feature-related skips.
- [ ] `CoreFeaturesV2` and `SupportedFeatures` accurately describe the runtime.
- [ ] `.wago` loading rejects incompatible or unsupported reference metadata
  safely.
- [ ] Performance measurements show no unjustified regression to table-0,
  scalar-call, compile, instantiation, or footprint-sensitive paths.
- [ ] `FEATURES.md`, `ROADMAP.md`, `README.md`, and relevant developer docs match
  the implemented behavior.

## Engineering Constraints

- Keep malformed module rejection strict.
- Preserve the no-cgo runtime boundary.
- Do not place untracked Go pointers in native Wasm storage.
- Avoid process-global or otherwise unbounded reference caches.
- Preserve the table-0 and `call_indirect` hot paths unless measurements justify
  a regression.
- Keep table entry layouts type-specific to avoid wasting 32 bytes per externref.
- Add each feature as the smallest coherent, tested PR.
- Include benchmark and footprint numbers for runtime-layout or call-path
  changes.
